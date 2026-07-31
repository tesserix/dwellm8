package store_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0006 §5, against a real database.
//
// The arithmetic is tested in internal/money/domain without one. What needs
// PostgreSQL is that the engine writes what the arithmetic produced, once, and
// that a balance derived from those rows is the position the product will show.

// fixture is one lease on a unit of its own, so runs cannot collide on the
// no-double-let constraint and a balance cannot pick up the last run's postings.
type fixture struct {
	t      *testing.T
	ledger *store.Ledger
	ctx    context.Context
	lease  string
	place  domain.Place
	tenant string
	owner  string
	token  string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	reqPool, platPool := pool(t), platformPool(t)
	isolationtest.SeedPropertyTree(t, tenancy.NewPlatformPool(platPool))

	tok := runToken(t)
	unit, lease, party := uuidLike(tok, "1"), uuidLike(tok, "2"), uuidLike(tok, "3")

	err := tenancy.Platform(context.Background(), tenancy.NewPlatformPool(platPool),
		"seeding the posting-engine contract", func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, floor, carpet_area_sqft)
				VALUES ($1, $2, $3, 'flat', $4, 3, 600.00)`,
				unit, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, "LDG-"+tok[:6]); err != nil {
				return fmt.Errorf("seeding the unit: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO leases (id, tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES ($1, $2, $3, $4, 'active', date '2026-01-01', date '2026-12-31')`,
				lease, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unit); err != nil {
				return err
			}
			// ADR-0024: a tenancy does not start without the two facts that decide
			// which TDS section governs every payment made under it.
			return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgOwner.String(),
				lease, "2026-01-01")
		})
	if err != nil {
		t.Fatalf("seeding the lease: %v", err)
	}

	return fixture{
		t:      t,
		ledger: store.NewLedger(reqPool),
		ctx:    tenancy.With(context.Background(), isolationtest.OrgOwner),
		lease:  lease,
		place:  domain.Place{Property: isolationtest.PropertyGranted, Unit: unit},
		tenant: party,
		owner:  isolationtest.OrgOwner.String(),
		token:  tok,
	}
}

func runToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("token: %v", err)
	}
	return hex.EncodeToString(b)
}

// uuidLike builds a valid uuid from the run token, distinct per nth — which must
// be one hex digit, because anything past the 36th character is not in the uuid.
func uuidLike(tok, nth string) string {
	return fmt.Sprintf("%s-%s-4000-8000-%s%s", tok[:8], tok[8:12], nth, tok[:11])
}

func date(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func (f fixture) src(kind, id string, on string) domain.Source {
	return domain.Source{
		Kind:           kind,
		ID:             id,
		IdempotencyKey: f.token + "-" + id,
		OccurredOn:     date(on),
		Lease:          f.lease,
	}
}

// post takes the two results of a template call directly, so a test reads as the
// events it is describing.
func (f fixture) post(e domain.Entry, err error) store.Posted {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("building the entry: %v", err)
	}
	p, err := f.ledger.Post(f.ctx, e)
	if err != nil {
		f.t.Fatalf("posting a %s: %v", e.Kind, err)
	}
	return p
}

func (f fixture) position(on string) domain.Minor {
	f.t.Helper()
	got, err := f.ledger.Position(f.ctx, f.lease, date(on))
	if err != nil {
		f.t.Fatalf("position as of %s: %v", on, err)
	}
	return got
}

// The story's primary scenario: an invoice, a partial payment, a late fee and a
// full settlement, with the position asserted at each intermediate date.
func TestALeasePositionIsTheChargesLessWhatWasPaid(t *testing.T) {
	f := newFixture(t)

	f.post(domain.Invoice(2_000_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "invoice-jan", "2026-01-01")))
	f.post(domain.Payment(1_200_000, 2_000_000, f.place, f.tenant,
		f.src("payment", "receipt-1", "2026-01-10")))
	f.post(domain.LateFee(50_000, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "late-fee-jan", "2026-01-15")))
	f.post(domain.Payment(850_000, 850_000, f.place, f.tenant,
		f.src("payment", "receipt-2", "2026-01-20")))

	for _, c := range []struct {
		on   string
		want domain.Minor
	}{
		{"2025-12-31", 0},
		{"2026-01-01", 2_000_000},
		{"2026-01-10", 800_000},
		{"2026-01-15", 850_000},
		{"2026-01-20", 0},
	} {
		if got := f.position(c.on); got != c.want {
			t.Errorf("position as of %s is %s, want %s", c.on, got, c.want)
		}
	}
}

// The other half of the same scenario: every account of the lease sums to zero,
// which is the only assertion that catches a template posting to one side only.
func TestTheLedgerBalancesInAggregate(t *testing.T) {
	f := newFixture(t)

	f.post(domain.Invoice(1_800_000, 200_000, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "invoice", "2026-02-01")))
	f.post(domain.Payment(2_000_000, 2_000_000, f.place, f.tenant,
		f.src("payment", "receipt", "2026-02-05")))
	f.post(domain.Settlement(2_000_000, f.place,
		f.src("settlement", "payout", "2026-02-07")))

	balances, err := f.ledger.Balances(f.ctx, store.BalanceQuery{Lease: f.lease})
	if err != nil {
		t.Fatalf("deriving balances: %v", err)
	}
	if len(balances) == 0 {
		t.Fatal("no balances for a lease that has three entries against it")
	}
	var total domain.Minor
	for _, b := range balances {
		total += b.Amount
	}
	if total != 0 {
		t.Errorf("the lease's accounts sum to %s, want 0 — an entry posted to one side only", total)
	}
}

// The idempotency guarantee, which is what stops a redelivered webhook from
// posting a second set of lines. Issue #42's primary scenario depends on it.
func TestTheSameKeyPostsOneEntry(t *testing.T) {
	f := newFixture(t)

	e, err := domain.Invoice(500_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "invoice", "2026-03-01"))
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	first := f.post(e, nil)
	if first.Duplicate {
		t.Fatal("the first post reports a duplicate")
	}
	for i := range 4 {
		again, err := f.ledger.Post(f.ctx, e)
		if err != nil {
			t.Fatalf("redelivery %d: %v", i, err)
		}
		if !again.Duplicate {
			t.Errorf("redelivery %d wrote a second entry", i)
		}
		if again.ID != first.ID {
			t.Errorf("redelivery %d resolved to entry %s, want %s", i, again.ID, first.ID)
		}
	}
	if got := f.position("2026-03-01"); got != 500_000 {
		t.Errorf("position is %s after one invoice and four redeliveries, want %s",
			got, domain.Minor(500_000))
	}
}

// An unbalanced entry is refused with nothing persisted. The domain rejects it
// before the database is asked; the deferred trigger is the backstop for anything
// that never came through Go, and isolationtest asserts that one.
func TestAnEntryThatDoesNotBalanceWritesNothing(t *testing.T) {
	f := newFixture(t)

	before := f.position("2026-04-30")
	_, err := f.ledger.Post(f.ctx, domain.Entry{
		Kind:           domain.KindInvoice,
		OccurredOn:     date("2026-04-01"),
		Property:       f.place.Property,
		Unit:           f.place.Unit,
		Lease:          f.lease,
		SourceKind:     domain.SourceLeaseCharge,
		SourceID:       "unbalanced",
		IdempotencyKey: f.token + "-unbalanced",
		Postings: []domain.Posting{
			{Account: domain.TenantReceivable, Side: domain.Debit, Amount: 900_000,
				Party: domain.Party{Kind: domain.Tenant, ID: f.tenant}},
			{Account: domain.RentIncome, Side: domain.Credit, Amount: 400_000,
				Party: domain.Party{Kind: domain.Owner, ID: f.owner}},
		},
	})
	if !errors.Is(err, domain.ErrUnbalanced) {
		t.Fatalf("posting an unbalanced entry returned %v, want ErrUnbalanced", err)
	}
	if after := f.position("2026-04-30"); after != before {
		t.Errorf("the position moved from %s to %s on a refused entry", before, after)
	}
}

// A correction is a reversing entry, and only ever one of them.
func TestACorrectionIsAReversalAndHappensOnce(t *testing.T) {
	f := newFixture(t)

	wrong := f.post(domain.Invoice(700_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "invoice-wrong", "2026-05-01")))
	if got := f.position("2026-05-01"); got != 700_000 {
		t.Fatalf("position is %s after the invoice, want %s", got, domain.Minor(700_000))
	}

	if _, err := f.ledger.Reverse(f.ctx, wrong.ID, domain.ReasonWrongAmount,
		f.token+"-reversal"); err != nil {
		t.Fatalf("reversing: %v", err)
	}
	if got := f.position("2026-05-01"); got != 0 {
		t.Errorf("position is %s after the reversal, want 0", got)
	}

	_, err := f.ledger.Reverse(f.ctx, wrong.ID, domain.ReasonWrongAmount, f.token+"-reversal-2")
	if !errors.Is(err, store.ErrAlreadyReversed) {
		t.Errorf("a second reversal returned %v, want ErrAlreadyReversed", err)
	}
}

// ADR-0010 §7: a charge that does not name its tenancy leaves the
// retrospective-termination guard with nothing to read.
func TestALeaseChargeMustNameItsLease(t *testing.T) {
	f := newFixture(t)

	src := f.src(domain.SourceLeaseCharge, "unattached", "2026-06-01")
	src.Lease = ""
	_, err := domain.Invoice(300_000, 0, f.place, f.tenant, f.owner, src)
	if err == nil {
		t.Fatal("an invoice calling itself a lease charge was built without a lease")
	}
}

// Concurrent postings against one lease must not interleave into a position that
// is neither the before nor the after. Each writes its own entry; the sum is what
// is asserted, because that is what the product reads.
func TestConcurrentPostingsAgainstOneLeaseAgree(t *testing.T) {
	f := newFixture(t)
	const posts = 8
	const each = domain.Minor(125_000)

	var wg sync.WaitGroup
	errs := make(chan error, posts)
	for i := range posts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := domain.Invoice(each, 0, f.place, f.tenant, f.owner,
				f.src(domain.SourceLeaseCharge, fmt.Sprintf("concurrent-%d", i), "2026-07-01"))
			if err != nil {
				errs <- err
				return
			}
			if _, err := f.ledger.Post(f.ctx, e); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent post: %v", err)
	}

	if got, want := f.position("2026-07-01"), each*posts; got != want {
		t.Errorf("position after %d concurrent invoices is %s, want %s", posts, got, want)
	}
}

// A balance is bounded by the accounting date, not by when the row was written,
// so a backdated entry lands in the period it belongs to.
func TestABackdatedEntryLandsInItsOwnPeriod(t *testing.T) {
	f := newFixture(t)

	f.post(domain.Invoice(400_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "current", "2026-09-01")))
	f.post(domain.Invoice(600_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "backdated", "2026-08-01")))

	if got := f.position("2026-08-15"); got != 600_000 {
		t.Errorf("position as of 15 August is %s, want %s — the backdated entry is in the wrong period",
			got, domain.Minor(600_000))
	}
	if got := f.position("2026-09-30"); got != 1_000_000 {
		t.Errorf("position as of 30 September is %s, want %s", got, domain.Minor(1_000_000))
	}
}
