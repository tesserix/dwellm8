package e2e_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/e2e"
	leasedomain "github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// The run's own behaviour, without a database: what it does when one tenancy in
// the middle of the list cannot be billed.

type tenancies struct{ leases []leasedomain.Lease }

func (t *tenancies) Billable(context.Context, int) ([]leasedomain.Lease, error) {
	return t.leases, nil
}

type charges struct{ failFor string }

func (c *charges) Billable(_ context.Context, l leasedomain.Lease, _ effective.Date) ([]leaseservice.Charge, error) {
	if l.ID == c.failFor {
		return nil, errors.New("this tenancy has no recorded owner")
	}
	return []leaseservice.Charge{{LeaseID: l.ID, FullAmountMinor: 100, Days: 1, InPeriod: 1}}, nil
}

type biller struct{ billed int }

func (b *biller) Bill(_ context.Context, cs []leaseservice.Charge) (moneyservice.Run, error) {
	b.billed += len(cs)
	return moneyservice.Run{Raised: len(cs), TotalMinor: 100}, nil
}

// One bad tenancy does not cost the rest their month.
func TestOneUnbillableTenancyDoesNotStopTheRun(t *testing.T) {
	list := &tenancies{leases: []leasedomain.Lease{{ID: "a"}, {ID: "broken"}, {ID: "c"}}}
	b := &biller{}
	run := e2e.NewBillingRun(list, &charges{failFor: "broken"}, b,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	out, err := run.Run(context.Background(), effective.Day(2029, 9, 30), 0)
	if err != nil {
		t.Fatalf("the run stopped: %v", err)
	}
	if out.Tenancies != 3 {
		t.Errorf("the run considered %d tenancies, want all three", out.Tenancies)
	}
	if out.Raised != 2 || out.Failed != 1 {
		t.Errorf("the run produced %+v, want two raised and one failed — a month's rent must not "+
			"go unbilled because of one bad row", out)
	}
	if b.billed != 2 {
		t.Errorf("the biller saw %d charges", b.billed)
	}
}
