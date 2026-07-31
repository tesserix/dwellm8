package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
)

// The polling sweep. ADR-0011 built the delivery path and this is the other
// half — the same confirmation, on a different trigger, so the two cannot
// collect twice.

type pending struct {
	rows []collect.Payment
	err  error
	// asked records the cut-off the sweep passed, so the age filter is asserted
	// rather than assumed.
	asked time.Time
	limit int
}

func (p *pending) Pending(_ context.Context, olderThan time.Time, limit int) ([]collect.Payment, error) {
	p.asked, p.limit = olderThan, limit
	return p.rows, p.err
}

// failing answers every confirmation with an error, to prove one unreachable
// order does not leave every payment behind it unasked.
type failing struct{ asked int }

func (f *failing) Name() string { return "fake" }
func (f *failing) CreateOrder(context.Context, provider.OrderRequest) (provider.Order, error) {
	return provider.Order{}, nil
}
func (f *failing) Confirm(context.Context, string) (provider.Confirmation, error) {
	f.asked++
	return provider.Confirmation{}, errors.New("the provider is unreachable")
}
func (f *failing) VerifyWebhook(provider.Webhook) bool { return true }
func (f *failing) Supports(collect.Method) bool        { return true }

// A sweep asks only about payments old enough to have plausibly finished.
func TestASweepLeavesRecentPaymentsAlone(t *testing.T) {
	h := newHarness(t)
	store := &pending{}

	if _, err := h.svc.Sweep(context.Background(), store, 5*time.Minute, 0); err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	if store.asked.IsZero() {
		t.Fatal("the sweep asked for everything, with no cut-off")
	}
	if time.Since(store.asked) < 4*time.Minute {
		t.Errorf("the cut-off is %s, which is inside the settle window — a payment created two "+
			"seconds ago is a tenant still looking at their UPI app", store.asked)
	}
	if store.limit <= 0 {
		t.Errorf("the sweep asked for %d payments — an unbounded sweep reads the table", store.limit)
	}
}

// A settled payment is never asked about again: that would be a request per
// settled payment per sweep, forever.
func TestATerminalPaymentIsNotPolled(t *testing.T) {
	h := newHarness(t)

	out, err := h.svc.Poll(context.Background(), collect.Payment{
		ID: "p1", Provider: "fake", Status: collect.StatusCaptured,
	})
	if err != nil {
		t.Fatalf("polling: %v", err)
	}
	if out.Status != collect.StatusCaptured {
		t.Errorf("the poll changed a captured payment to %s", out.Status)
	}
}

// One provider error does not stop the sweep, or a single stuck order turns
// into every payment behind it going unasked.
func TestOneFailureDoesNotStopTheSweep(t *testing.T) {
	h := newHarness(t)
	// Its own service over an adapter that always errors: the harness's fake
	// answers happily, and what is being tested is what a sweep does when the
	// provider does not.
	svc := h.withProvider(t, &failing{})

	store := &pending{rows: []collect.Payment{
		{ID: "p1", Provider: "fake", Status: collect.StatusAttempted, Amount: 100},
		{ID: "p2", Provider: "fake", Status: collect.StatusAttempted, Amount: 100},
	}}

	out, err := svc.Sweep(context.Background(), store, time.Minute, 10)
	if err != nil {
		t.Fatalf("the sweep stopped on a provider error: %v", err)
	}
	if out.Asked != 2 || out.Failed != 2 {
		t.Errorf("swept %+v, want both asked and both recorded as failures", out)
	}
}
