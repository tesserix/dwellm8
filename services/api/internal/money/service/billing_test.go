package service_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/service"
)

// Invoicing. The arithmetic is tested without a database, because it is
// arithmetic; the idempotency is tested with one, because it is a unique index.

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func charge(from string, days, inPeriod int, full int64) leaseservice.Charge {
	start, _ := time.Parse("2006-01-02", from)
	return leaseservice.Charge{
		LeaseID: "lease-1", Property: "prop-1", Unit: "unit-1",
		Tenant: "tenant-1", Owner: "owner-1",
		From: start, To: start.AddDate(0, 1, 0), DueOn: start,
		FullAmountMinor: full, Days: days, InPeriod: inPeriod,
		Reference: "Rent",
	}
}

// A whole period costs the whole rent, and nothing divides.
func TestAWholePeriodIsNotProrated(t *testing.T) {
	got, err := service.Amount(charge("2029-08-05", 31, 31, 27_500_00))
	if err != nil {
		t.Fatalf("pricing: %v", err)
	}
	if got != 27_500_00 {
		t.Errorf("a whole period cost %s, want the full rent", got)
	}
}

// The story's edge case, to the paisa: a lease starting on the 17th of a
// 31-day month. The two slices must sum to the whole, or the owner is paid
// less than the rent for a month somebody occupied in full.
func TestTheSlicesOfAMonthSumToTheWholeRent(t *testing.T) {
	const full = 27_500_00 // ₹27,500 over 31 days does not divide evenly

	// 17 August to 5 September is 19 days of the 31-day period 5 Aug – 5 Sep;
	// the part before it is the other 12.
	occupied, err := service.Amount(charge("2029-08-17", 19, 31, full))
	if err != nil {
		t.Fatalf("pricing the occupied part: %v", err)
	}
	vacant, err := service.Amount(charge("2029-08-05", 12, 31, full))
	if err != nil {
		t.Fatalf("pricing the rest: %v", err)
	}

	if occupied+vacant != full {
		t.Errorf("the slices are %s and %s and sum to %s, not %s — a month split in two must "+
			"cost what a month costs", occupied, vacant, occupied+vacant, domain.Minor(full))
	}
	// And neither is a rounded-down approximation: 19/31 of ₹27,500 is
	// ₹16,854.8387…, which rounds to ₹16,854.84.
	if occupied != 16_854_84 {
		t.Errorf("19 of 31 days cost %s, want ₹16,854.84", occupied)
	}
}

// Proration is exact across every split of a period, not just the convenient
// one. A rent that divides badly is the normal case.
func TestEverySplitOfAPeriodSumsToTheWhole(t *testing.T) {
	for _, full := range []int64{27_500_00, 33_333_33, 1_00, 9_99_999_99} {
		for days := 1; days < 31; days++ {
			a, err := service.Amount(charge("2029-08-05", days, 31, full))
			if err != nil {
				t.Fatalf("pricing: %v", err)
			}
			b, err := service.Amount(charge("2029-08-05", 31-days, 31, full))
			if err != nil {
				t.Fatalf("pricing: %v", err)
			}
			// Half-away-from-zero rounding on both halves can differ from the whole
			// by at most one paisa, and the assertion is that it does not exceed
			// that — a systematic drift would show up as a growing gap.
			diff := a + b - domain.Minor(full)
			if diff < -1 || diff > 1 {
				t.Fatalf("%d/31 and %d/31 of %s sum to %s — off by %s",
					days, 31-days, domain.Minor(full), a+b, diff)
			}
		}
	}
}

// A period covering no days is not an invoice. Posting one would put a line in
// the ledger meaning nothing happened, and send the tenant a document to dismiss.
func TestAnEmptyPeriodIsNotAnInvoice(t *testing.T) {
	h := newHarness(t)
	biller := service.NewBiller(h.ledger, discardLog(), nil)

	if _, err := biller.Raise(context.Background(), charge("2029-08-05", 0, 31, 27_500_00)); err == nil {
		t.Error("a period covering no days was invoiced")
	}
	if _, err := biller.Raise(context.Background(), charge("2029-08-05", 31, 31, 0)); err == nil {
		t.Error("a rent of nothing was invoiced")
	}
}

// The idempotency key is the lease and the period, so it survives a due date
// being revised — and two different periods of one lease are two invoices.
func TestTheKeyIsThePeriodRatherThanTheDueDate(t *testing.T) {
	august := charge("2029-08-05", 31, 31, 27_500_00)
	revised := august
	revised.DueOn = revised.DueOn.AddDate(0, 0, 3)
	if august.Key() != revised.Key() {
		t.Error("moving the due date changed the key — the same period would be invoiced twice")
	}

	september := charge("2029-09-05", 30, 30, 27_500_00)
	if september.Key() == august.Key() {
		t.Error("two periods share one key — the second month would never be invoiced")
	}
}
