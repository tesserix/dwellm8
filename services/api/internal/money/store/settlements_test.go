package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The division of one collection against PostgreSQL (#270). What is worth
// catching here is that a payment is divided once — the payout run reads this
// table, and a second row would pay the owner twice.

func seedPayment(t *testing.T, f fixture, token string) string {
	t.Helper()
	var id string
	err := tenancy.Platform(context.Background(), tenancy.NewPlatformPool(platformPool(t)),
		"seeding a payment", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO payments (tenant_id, property_id, unit_id, lease_id, payer_kind, payer_id,
				                      amount_minor, method, provider, status, idempotency_key)
				VALUES ($1, $2, $3, $4, 'tenant', $5, 3200000, 'upi_intent', 'cashfree', 'created', $6)
				RETURNING id`,
				f.owner, f.place.Property, f.place.Unit, f.lease, f.tenant, token).Scan(&id)
		})
	if err != nil {
		t.Fatalf("seeding the payment: %v", err)
	}
	return id
}

func split() domain.Split {
	return domain.Split{
		Gross: 3200000, Platform: 112964, Management: 256000, TDS: 64000,
		Owner: 3200000 - 112964 - 256000 - 64000, RuleID: "r1",
	}
}

func TestOneCollectionIsDividedOnceAndReadBack(t *testing.T) {
	f := newFixture(t)
	s := store.NewSettlements(pool(t))
	payment := seedPayment(t, f, "split-"+f.token)
	expected := time.Now().AddDate(0, 0, 1)

	saved, err := s.Record(f.ctx, store.Instruction{
		PaymentID: payment, LeaseID: f.lease, Currency: "INR",
		Split: split(), Provider: "cashfree", ExpectedOn: expected,
	})
	if err != nil {
		t.Fatalf("recording the division: %v", err)
	}
	if saved.State != store.SettlementPending || saved.Split.Owner != split().Owner {
		t.Fatalf("recorded = %+v", saved)
	}

	// Capture is retried; the division must not be.
	again, err := s.Record(f.ctx, store.Instruction{
		PaymentID: payment, LeaseID: f.lease, Currency: "INR",
		Split: split(), Provider: "cashfree", ExpectedOn: expected,
	})
	if err != nil {
		t.Fatalf("recording twice: %v", err)
	}
	if again.ID != saved.ID {
		t.Fatalf("a second division was written: %s then %s", saved.ID, again.ID)
	}

	got, err := s.ForPayment(f.ctx, payment)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if got.Split.Management != 256000 || got.Split.TDS != 64000 {
		t.Fatalf("read back = %+v", got.Split)
	}
}

// The owner's leg walks: instructed when a transfer is sent, settled when the
// provider's report agrees. A settlement with no reference is not a settlement.
func TestTheOwnersLegWalksToSettled(t *testing.T) {
	f := newFixture(t)
	s := store.NewSettlements(pool(t))
	payment := seedPayment(t, f, "walk-"+f.token)

	saved, err := s.Record(f.ctx, store.Instruction{
		PaymentID: payment, LeaseID: f.lease, Currency: "INR", Split: split(),
		Provider: "cashfree", ExpectedOn: time.Now().AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("recording: %v", err)
	}

	if err := s.Instructed(f.ctx, saved.ID, "TRF-1"); err != nil {
		t.Fatalf("instructing: %v", err)
	}
	if err := s.Settled(f.ctx, saved.ID, "UTR123", time.Now()); err != nil {
		t.Fatalf("settling: %v", err)
	}

	got, err := s.ForPayment(f.ctx, payment)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if got.State != store.SettlementSettled || got.TransferRef != "UTR123" {
		t.Fatalf("settled = %+v; want the reference reconciliation matches on", got)
	}
}

// What is owed and unpaid is the payout run's queue, and the manager's
// exception list when a date passes.
func TestDueListsWhatHasNotSettled(t *testing.T) {
	f := newFixture(t)
	s := store.NewSettlements(pool(t))
	payment := seedPayment(t, f, "due-"+f.token)

	saved, err := s.Record(f.ctx, store.Instruction{
		PaymentID: payment, LeaseID: f.lease, Currency: "INR", Split: split(),
		Provider: "cashfree", ExpectedOn: time.Now().AddDate(0, 0, -2),
	})
	if err != nil {
		t.Fatalf("recording: %v", err)
	}

	due, err := s.Due(f.ctx, time.Now())
	if err != nil {
		t.Fatalf("listing what is due: %v", err)
	}
	var found bool
	for _, d := range due {
		if d.ID == saved.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("an overdue division was not in the queue of %d", len(due))
	}

	if err := s.Failed(f.ctx, saved.ID, "the bank returned the transfer"); err != nil {
		t.Fatalf("failing: %v", err)
	}
	got, _ := s.ForPayment(f.ctx, payment)
	if got.State != store.SettlementFailed || got.Reason == "" {
		t.Fatalf("failed = %+v; want the provider's words", got)
	}
}

// What the manager and the platform keep is the manager's business alone.
func TestAnotherOrganisationCannotReadTheDivision(t *testing.T) {
	f := newFixture(t)
	s := store.NewSettlements(pool(t))
	payment := seedPayment(t, f, "iso-"+f.token)

	if _, err := s.Record(f.ctx, store.Instruction{
		PaymentID: payment, LeaseID: f.lease, Currency: "INR", Split: split(),
		Provider: "cashfree", ExpectedOn: time.Now(),
	}); err != nil {
		t.Fatalf("recording: %v", err)
	}

	outsider := tenancy.With(t.Context(), isolationtest.OrgOutsider)
	if _, err := s.ForPayment(outsider, payment); err == nil {
		t.Fatal("an outsider read the division")
	}
}
