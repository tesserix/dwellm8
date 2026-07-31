package mandate_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/mandate"
)

// The state machine, stated rather than derived. If the table in mandate.go
// changes, this fails — which is the point: an authority's lifecycle is not
// something to adjust while fixing something else.
func TestTheStateMachineIsWhatItSaysItIs(t *testing.T) {
	allowed := map[mandate.Status][]mandate.Status{
		mandate.StatusCreated: {mandate.StatusPending, mandate.StatusRejected,
			mandate.StatusExpired, mandate.StatusRevoked},
		mandate.StatusPending: {mandate.StatusActive, mandate.StatusRejected,
			mandate.StatusExpired, mandate.StatusRevoked},
		mandate.StatusActive: {mandate.StatusPaused, mandate.StatusRevoked, mandate.StatusExpired},
		mandate.StatusPaused: {mandate.StatusActive, mandate.StatusRevoked, mandate.StatusExpired},

		mandate.StatusRejected: {},
		mandate.StatusRevoked:  {},
		mandate.StatusExpired:  {},
	}

	if len(allowed) != len(mandate.Statuses()) {
		t.Fatalf("the vocabulary has %d statuses and this table names %d",
			len(mandate.Statuses()), len(allowed))
	}

	for _, from := range mandate.Statuses() {
		want := map[mandate.Status]bool{from: true} // self-transition, always
		for _, to := range allowed[from] {
			want[to] = true
		}
		for _, to := range mandate.Statuses() {
			if got := mandate.CanTransition(from, to); got != want[to] {
				t.Errorf("%s -> %s = %v, want %v", from, to, got, want[to])
			}
		}
	}
}

// Pausing and resuming is the one place this package deliberately differs from
// ADR-0011's forward-only payments, so it is asserted rather than left to be
// discovered.
func TestAnAuthorityResumesWithoutBeingRegisteredAgain(t *testing.T) {
	m := &mandate.Mandate{ID: "m1", Status: mandate.StatusActive}
	at := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)

	if err := m.ApplyConfirmed(mandate.StatusPaused, at); err != nil {
		t.Fatalf("pausing: %v", err)
	}
	if m.Status.IsDebitable() {
		t.Error("a paused mandate is debitable")
	}
	if err := m.ApplyConfirmed(mandate.StatusActive, at.Add(time.Hour)); err != nil {
		t.Fatalf("resuming: %v — a tenant off a payment holiday would have to re-authorise", err)
	}
	if !m.Status.IsDebitable() {
		t.Error("a resumed mandate is not debitable")
	}
}

func TestActivatedAtIsSetOnceAndSurvivesAPause(t *testing.T) {
	first := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	m := &mandate.Mandate{ID: "m1", Status: mandate.StatusPending}

	_ = m.ApplyConfirmed(mandate.StatusActive, first)
	_ = m.ApplyConfirmed(mandate.StatusPaused, first.Add(30*24*time.Hour))
	_ = m.ApplyConfirmed(mandate.StatusActive, first.Add(60*24*time.Hour))

	if !m.ActivatedAt.Equal(first) {
		t.Errorf("ActivatedAt = %s, want %s — the tenant authorised it when they authorised it",
			m.ActivatedAt, first)
	}
}

func TestTerminalStatesAbsorb(t *testing.T) {
	at := time.Now()
	for _, terminal := range []mandate.Status{
		mandate.StatusRevoked, mandate.StatusExpired, mandate.StatusRejected,
	} {
		if !terminal.IsTerminal() {
			t.Errorf("%s is not terminal", terminal)
		}
		m := &mandate.Mandate{ID: "m1", Status: terminal}
		err := m.ApplyConfirmed(mandate.StatusActive, at)
		if !errors.Is(err, mandate.ErrStaleTransition) {
			t.Errorf("%s -> active returned %v", terminal, err)
		}
		if m.Status != terminal {
			t.Errorf("a late delivery revived a %s mandate", terminal)
		}
		// The redelivery of the event that ended it is a no-op, not an error.
		if err := m.ApplyConfirmed(terminal, at); err != nil {
			t.Errorf("redelivered %s: %v", terminal, err)
		}
	}
}

func TestTheCeilingIsCheckedBeforeTheProviderIsAsked(t *testing.T) {
	m := mandate.Mandate{ID: "m1", Status: mandate.StatusActive, MaxAmount: 1_500_000}

	if err := m.CanDebit(1_200_000); err != nil {
		t.Errorf("a debit within the ceiling was refused: %v", err)
	}
	if err := m.CanDebit(1_500_000); err != nil {
		t.Errorf("a debit exactly at the ceiling was refused: %v", err)
	}
	if err := m.CanDebit(1_500_001); err == nil {
		t.Error("a debit above the ceiling was allowed, and it would fail at the rail as a message to the tenant")
	}
	paused := m
	paused.Status = mandate.StatusPaused
	if err := paused.CanDebit(1_000); err == nil {
		t.Error("a paused mandate was debited")
	}
}

func TestValidateRefusesAnAuthorityNobodyGave(t *testing.T) {
	good := mandate.Mandate{
		ID: "m1", TenantID: "org1", Property: "p1", Unit: "u1",
		PayerKind: domain.Tenant, PayerID: "t1",
		Rail: mandate.RailUPIAutopay, MaxAmount: 1_500_000,
		Provider: "cashfree", ProviderMandateID: "mnd-1",
		Status: mandate.StatusActive,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("a complete mandate was refused: %v", err)
	}

	for name, mangle := range map[string]func(*mandate.Mandate){
		"no organisation":     func(m *mandate.Mandate) { m.TenantID = "" },
		"no property":         func(m *mandate.Mandate) { m.Property = "" },
		"no unit":             func(m *mandate.Mandate) { m.Unit = "" },
		"unknown rail":        func(m *mandate.Mandate) { m.Rail = "carrier_pigeon" },
		"unknown status":      func(m *mandate.Mandate) { m.Status = "probably_fine" },
		"no ceiling":          func(m *mandate.Mandate) { m.MaxAmount = 0 },
		"an owner as payer":   func(m *mandate.Mandate) { m.PayerKind = domain.Owner },
		"no payer":            func(m *mandate.Mandate) { m.PayerID = "" },
		"no provider":         func(m *mandate.Mandate) { m.Provider = "" },
		"active with no id":   func(m *mandate.Mandate) { m.ProviderMandateID = "" },
		"ends before it runs": func(m *mandate.Mandate) { m.FirstDebitOn = time.Now(); m.EndsOn = time.Now().Add(-time.Hour) },
	} {
		m := good
		mangle(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The same webhook rule as ADR-0011 §4, applied to an authority. The assertion
// that matters is the last one: no disposition writes a status.
func TestAMandateWebhookIsAdvisory(t *testing.T) {
	active := &mandate.Mandate{ID: "m1", Status: mandate.StatusActive}

	unsigned := mandate.Delivery{Claimed: mandate.StatusRevoked}
	if d := mandate.Decide(unsigned, active); d.Disposition != collect.Park ||
		d.Reason != collect.ParkSignatureInvalid {
		t.Errorf("an unsigned delivery decided %+v", d)
	}
	// An unsigned delivery for an unknown mandate is parked on the signature, not
	// on the mandate: a prober must not learn which authorities exist.
	if d := mandate.Decide(unsigned, nil); d.Reason != collect.ParkSignatureInvalid {
		t.Errorf("an unsigned delivery for an unknown mandate reported %s", d.Reason)
	}

	signed := func(claim mandate.Status) mandate.Delivery {
		return mandate.Delivery{SignatureVerified: true, Claimed: claim, ProviderMandateID: "mnd-1"}
	}

	for name, tc := range map[string]struct {
		d       mandate.Delivery
		current *mandate.Mandate
		want    collect.Disposition
		reason  collect.ParkReason
	}{
		"unknown claim":     {signed("bank_thinking_about_it"), active, collect.Park, collect.ParkUnsupportedEvent},
		"unknown mandate":   {signed(mandate.StatusActive), nil, collect.Park, collect.ParkUnknownPayment},
		"redelivery":        {signed(mandate.StatusActive), active, collect.Ignore, ""},
		"out of order":      {signed(mandate.StatusPending), active, collect.Park, collect.ParkStaleTransition},
		"a real transition": {signed(mandate.StatusPaused), active, collect.Confirm, ""},
	} {
		got := mandate.Decide(tc.d, tc.current)
		if got.Disposition != tc.want || got.Reason != tc.reason {
			t.Errorf("%s: %+v, want %s/%s", name, got, tc.want, tc.reason)
		}
	}

	// Whatever any delivery says, the mandate has not moved. Decide has no path
	// that writes a status, and this is that claim asserted end to end.
	if active.Status != mandate.StatusActive {
		t.Errorf("a webhook moved a mandate to %s", active.Status)
	}
}
