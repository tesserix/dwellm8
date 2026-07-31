// Package e2e holds the tests that exercise a seam between two modules.
//
// They live here rather than in either module because ADR-0001 §3 forbids a
// module reaching another's store or domain — and the arch guard counts test
// files, deliberately: a fixture that crosses the boundary is a boundary that
// will be crossed. An end-to-end test is above both modules, like cmd/, so this
// is where it goes.
package e2e_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jackc/pgx/v5"
	leasedomain "github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The whole path, against PostgreSQL: a real tenancy, its schedule, the
// proration, and the postings. This is the test that would catch the two halves
// agreeing with themselves and not with each other.
//
// It is in the money package rather than the lease one because the assertion is
// about the ledger. It imports lease/service, which ADR-0001 permits — a module
// may call another's service and may not touch its store or domain.

type billing struct {
	t      *testing.T
	leases *leaseservice.Leases
	biller *moneyservice.Biller
	ctx    context.Context
	lease  leasedomain.Lease
}

func newBilling(t *testing.T, from effective.Date, rent int64) billing {
	t.Helper()
	req, platPool := pools(t)
	plat := tenancy.NewPlatformPool(platPool)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	isolationtest.SeedPropertyTree(t, plat)
	leases := leaseservice.NewLeases(leasestore.New(req), log)

	// A unit and an owner for it: an invoice credits rent income to somebody, and
	// a tenancy on a unit nobody owns is refused rather than credited to nobody.
	tok := time.Now().Format("150405.000000")
	var unitID string
	if err := tenancy.Platform(context.Background(), plat, "seeding a billable unit",
		func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 620) RETURNING id`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted,
				"BILL-"+tok).Scan(&unitID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
				                                share_bps, valid_from)
				VALUES ($1, $2, $3, gen_random_uuid(), 10000, $4::date)`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unitID,
				from.Time())
			return err
		}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	term, err := effective.Between(from, effective.Day(from.Time().Year()+1, from.Time().Month(), from.Time().Day()))
	if err != nil {
		t.Fatalf("term: %v", err)
	}
	created, err := leases.Create(ctx, leasedomain.Draft{
		TenantID: isolationtest.OrgOwner.String(),
		Property: isolationtest.PropertyGranted, Unit: unitID,
		Term: term, NoticeDays: 30,
		Terms: leasedomain.Terms{
			RentMinor: rent, Cycle: leasedomain.Monthly, DueDay: 5,
		},
		Parties: []leasedomain.Party{{
			PartyID: "22222222-3333-4444-5555-666666666666",
			Role:    leasedomain.RoleTenant, Name: "Ravi Menon", Phone: "+919876543210",
		}},
	})
	if err != nil {
		t.Fatalf("creating the lease: %v", err)
	}

	l := created.Lease
	l.ID = created.ID
	l.State = leasedomain.StateActive

	return billing{
		t: t, leases: leases, ctx: ctx, lease: l,
		biller: moneyservice.NewBiller(moneystore.NewLedger(req), log, nil),
	}
}

// The story's primary scenario: a tenancy at ₹27,500 due on the 5th, billed
// ahead of the due date, with the receivable posted once.
func TestTheSchedulerRaisesTheNextCycleWithItsReceivable(t *testing.T) {
	b := newBilling(t, effective.Day(2029, 8, 5), 27_500_00)

	// Five days ahead of the September due date.
	charges, err := b.leases.Billable(b.ctx, b.lease, effective.Day(2029, 8, 31))
	if err != nil {
		t.Fatalf("finding what is billable: %v", err)
	}
	if len(charges) != 1 {
		t.Fatalf("%d charges fell due by 31 August, want the one due on the 5th: %+v", len(charges), charges)
	}
	if charges[0].Partial() {
		t.Errorf("the first period is prorated at %d/%d — the tenancy starts on its own due day",
			charges[0].Days, charges[0].InPeriod)
	}

	run, err := b.biller.Bill(b.ctx, charges)
	if err != nil {
		t.Fatalf("billing: %v", err)
	}
	if run.Raised != 1 || run.TotalMinor != 27_500_00 {
		t.Fatalf("the run raised %+v, want one invoice for the full rent", run)
	}
}

// The story's failure scenario: the scheduler runs twice after a restart.
func TestASecondRunRaisesNothing(t *testing.T) {
	b := newBilling(t, effective.Day(2029, 8, 5), 27_500_00)

	charges, err := b.leases.Billable(b.ctx, b.lease, effective.Day(2029, 10, 31))
	if err != nil {
		t.Fatalf("finding what is billable: %v", err)
	}
	if len(charges) < 2 {
		t.Fatalf("%d charges by 31 October, want at least two", len(charges))
	}

	first, err := b.biller.Bill(b.ctx, charges)
	if err != nil {
		t.Fatalf("the first run: %v", err)
	}
	second, err := b.biller.Bill(b.ctx, charges)
	if err != nil {
		t.Fatalf("the second run: %v", err)
	}

	if second.Raised != 0 {
		t.Errorf("a second run raised %d invoices — a pod restart would double every tenant's "+
			"month", second.Raised)
	}
	if second.Duplicate != first.Raised {
		t.Errorf("the second run recognised %d of %d as already invoiced",
			second.Duplicate, first.Raised)
	}
	if second.TotalMinor != 0 {
		t.Errorf("the second run billed %s", second.TotalMinor)
	}
}

// A tenancy starting mid-period is billed for the part it occupies, on the day
// it starts.
func TestAMidPeriodStartIsBilledForWhatItOccupies(t *testing.T) {
	b := newBilling(t, effective.Day(2029, 8, 17), 27_500_00)

	charges, err := b.leases.Billable(b.ctx, b.lease, effective.Day(2029, 8, 31))
	if err != nil {
		t.Fatalf("finding what is billable: %v", err)
	}
	if len(charges) != 1 || !charges[0].Partial() {
		t.Fatalf("charges are %+v, want one partial", charges)
	}

	run, err := b.biller.Bill(b.ctx, charges)
	if err != nil {
		t.Fatalf("billing: %v", err)
	}
	// 17 August to 5 September is 19 days of a 31-day period.
	if run.TotalMinor != 16_854_84 {
		t.Errorf("the part period billed %s, want ₹16,854.84", run.TotalMinor)
	}
}

// The story's second edge case: a terminated tenancy stops.
func TestATerminatedTenancyGeneratesNothingFurther(t *testing.T) {
	b := newBilling(t, effective.Day(2029, 8, 5), 27_500_00)

	ended := b.lease
	ended.State = leasedomain.StateTerminated
	ended.EndedOn = effective.Day(2029, 9, 20)

	charges, err := b.leases.Billable(b.ctx, ended, effective.Day(2030, 1, 31))
	if err != nil {
		t.Fatalf("finding what is billable: %v", err)
	}
	if len(charges) != 0 {
		t.Errorf("a terminated tenancy produced %d charges — billing stops because the state "+
			"stopped, not because somebody remembered to check", len(charges))
	}
}

// pools connects as the request role and the platform role. Its own rather than
// borrowed from a module's test helpers, because those live in package-private
// test files — which is the same boundary this package exists to respect.
func pools(t *testing.T) (tenancy.Pool, tenancy.Pool) {
	t.Helper()
	dsn, plat := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" || plat == "" {
		t.Skip("TEST_DATABASE_URL and TEST_PLATFORM_DATABASE_URL are not set")
	}
	req, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(req.Close)
	p, err := pgxpool.New(context.Background(), plat)
	if err != nil {
		t.Fatalf("connecting as platform: %v", err)
	}
	t.Cleanup(p.Close)
	return req, p
}
