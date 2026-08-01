package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The period close (#190) against PostgreSQL, as one story, because the
// states chain: blocked by the checklist, closable once resolved, locked
// against backdated postings, correctable only in the current period, and
// reopenable with a reason.
//
// The months are 2024 on purpose — every other test in this package posts
// into 2026, and a close is tenant-wide state that survives this test.

func TestThePeriodCloseStory(t *testing.T) {
	f := newFixture(t)
	periods := store.NewPeriods(pool(t))
	plat := tenancy.NewPlatformPool(platformPool(t))
	month := date("2024-03-01")

	// An invoice in the month, before it closes.
	original := f.post(domain.Invoice(900_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "mar-2024-invoice", "2024-03-10")))

	// A settlement line the provider settled in the month, attributed to this
	// organisation and not yet posted: the checklist's known gap. Attribution
	// and a payment arrive together (settlement_lines_attribution_shape), so
	// the gap is a matched-to-payment line that has not caused an entry.
	lineID := ""
	err := tenancy.Platform(context.Background(), plat, "seeding the pre-close gap",
		func(ctx context.Context, tx pgx.Tx) error {
			var batch, payment string
			if err := tx.QueryRow(ctx, `
				INSERT INTO settlement_batches (provider, provider_batch_id, settled_on, gross_minor, net_minor)
				VALUES ('cashfree', $1, date '2024-03-05', 100000, 100000)
				RETURNING id`, "close-story-"+f.token).Scan(&batch); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO payments (tenant_id, property_id, unit_id, lease_id, payer_kind, payer_id,
				                      amount_minor, method, provider, status, idempotency_key)
				VALUES ($1, $2, $3, $4, 'tenant', $5, 100000, 'upi_intent', 'cashfree', 'created', $6)
				RETURNING id`,
				f.owner, f.place.Property, f.place.Unit, f.lease, f.tenant,
				"close-story-"+f.token).Scan(&payment); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `
				INSERT INTO settlement_lines (batch_id, tenant_id, payment_id, provider, provider_line_id,
				                              line_kind, direction, amount_minor, settled_on)
				VALUES ($1, $2, $3, 'cashfree', $4, 'payment', 'inward', 100000, date '2024-03-05')
				RETURNING id`, batch, f.owner, payment, "line-"+f.token).Scan(&lineID)
		})
	if err != nil {
		t.Fatalf("seeding the gap: %v", err)
	}

	var blocked store.ErrBlocked
	if err := periods.Close(f.ctx, month, "closer", ""); !errors.As(err, &blocked) {
		t.Fatalf("a close over a known gap answered %v, want the blocker list", err)
	}
	if len(blocked.Blockers) == 0 || blocked.Blockers[0].Check != "settlement_lines_unposted" {
		t.Fatalf("blockers: %+v", blocked.Blockers)
	}

	// Resolve the gap the way matching would, then the close goes through.
	err = tenancy.Platform(context.Background(), plat, "resolving the pre-close gap",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				UPDATE settlement_lines SET match_class = 'exact', entry_id = $2, matched_at = now()
				 WHERE id = $1`, lineID, original.ID)
			return err
		})
	if err != nil {
		t.Fatalf("resolving the gap: %v", err)
	}
	if err := periods.Close(f.ctx, month, "closer", ""); err != nil {
		t.Fatalf("a clean close refused: %v", err)
	}
	if err := periods.Close(f.ctx, month, "closer", ""); !errors.Is(err, store.ErrAlreadyClosed) {
		t.Fatalf("a second close answered %v, want ErrAlreadyClosed", err)
	}

	// The primary scenario: a posting dated inside the closed month is refused
	// by the database, distinguishably, so a caller can offer the adjustment.
	_, err = f.ledger.Post(f.ctx, mustEntry(domain.Invoice(100_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "mar-2024-late", "2024-03-20"))))
	if !store.IsClosedPeriod(err) {
		t.Fatalf("a backdated posting into a closed month answered %v, want the period guard", err)
	}

	// A plain reversal inherits the original's date — also refused. The
	// correction that works is the current-period adjustment, referencing the
	// original, dated in a month that is open.
	if _, err := f.ledger.Reverse(f.ctx, original.ID, domain.ReasonWrongPeriod,
		f.token+"-rev-in-closed"); !store.IsClosedPeriod(err) {
		t.Fatalf("a reversal dated into the closed month answered %v, want the period guard", err)
	}
	if _, err := f.ledger.ReverseAsOf(f.ctx, original.ID, domain.ReasonWrongPeriod,
		f.token+"-rev-current", date("2026-06-15")); err != nil {
		t.Fatalf("the current-period adjustment refused: %v", err)
	}

	// Reopen: rare, reasoned, audited — and the month takes postings again.
	if err := periods.Reopen(f.ctx, month, "reopener", "short"); err == nil {
		t.Fatal("a reopen with a throwaway reason must be refused by the schema")
	}
	if err := periods.Reopen(f.ctx, month, "reopener",
		"auditor found a missed adjustment for March"); err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if _, err := f.ledger.Post(f.ctx, mustEntry(domain.Invoice(100_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "mar-2024-after-reopen", "2024-03-21")))); err != nil {
		t.Fatalf("a posting into the reopened month refused: %v", err)
	}

	// The history holds the whole story, oldest first.
	state, err := periods.State(f.ctx, month)
	if err != nil {
		t.Fatal(err)
	}
	if state.Closed || len(state.History) != 2 ||
		state.History[0].Action != "closed" || state.History[1].Action != "reopened" {
		t.Fatalf("state: closed=%v history=%+v", state.Closed, state.History)
	}
}

func TestTheAuditPackAddsUp(t *testing.T) {
	f := newFixture(t)
	periods := store.NewPeriods(pool(t))

	f.post(domain.Invoice(1_500_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "jun-2024-invoice", "2024-06-01")))
	f.post(domain.Payment(1_500_000, 1_500_000, f.place, f.tenant,
		f.src("payment", "jun-2024-receipt", "2024-06-05")))

	pack, err := periods.Pack(f.ctx, date("2024-06-01"))
	if err != nil {
		t.Fatal(err)
	}

	// Double entry survives the export: the trial balance balances.
	var debits, credits int64
	for _, l := range pack.TrialBalance {
		debits += l.DebitMinor
		credits += l.CreditMinor
	}
	if debits == 0 || debits != credits {
		t.Fatalf("trial balance: debits %d, credits %d", debits, credits)
	}

	// Only this fixture posts into 2024-06 (this package's other tests use
	// 2026), so the pack is exactly the two entries, each with its lines.
	if len(pack.Entries) < 2 {
		t.Fatalf("entries: %d, want at least the invoice and the payment", len(pack.Entries))
	}
	for _, e := range pack.Entries {
		if len(e.Postings) < 2 {
			t.Fatalf("entry %s exported with %d posting(s) — not double entry", e.ID, len(e.Postings))
		}
	}
	if pack.Month != "2024-06" || pack.GeneratedAt.IsZero() {
		t.Fatalf("pack header: %s %v", pack.Month, pack.GeneratedAt)
	}
}

// mustEntry unwraps a template build whose validity is not what the test is
// about.
func mustEntry(e domain.Entry, err error) domain.Entry {
	if err != nil {
		panic(fmt.Sprintf("building the entry: %v", err))
	}
	return e
}
