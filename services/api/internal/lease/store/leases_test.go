package store_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Creating a lease against PostgreSQL, and the two refusals the database owns:
// one flat cannot be let twice over the same days, and a tenancy cannot start
// with no TDS section governing it.

func pools(t *testing.T) (tenancy.Pool, tenancy.PlatformPool) {
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
	return req, tenancy.NewPlatformPool(p)
}

// A unit of its own per test, because the no-double-let constraint is real and
// a shared unit would make these pass or fail for the wrong reason.
func unit(t *testing.T, plat tenancy.PlatformPool, code string) (string, string) {
	t.Helper()
	isolationtest.SeedPropertyTree(t, plat)

	var id string
	err := tenancy.Platform(context.Background(), plat, "seeding a lease unit",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 620)
				RETURNING id`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, code).Scan(&id)
		})
	if err != nil {
		t.Fatalf("seeding a unit: %v", err)
	}
	return isolationtest.PropertyGranted, id
}

func draft(t *testing.T, property, unitID string, from, to effective.Date, facts *tds.Facts) domain.Draft {
	t.Helper()
	term, err := effective.Between(from, to)
	if err != nil {
		t.Fatalf("term: %v", err)
	}
	d := domain.Draft{
		TenantID: isolationtest.OrgOwner.String(), Property: property, Unit: unitID,
		Term: term, NoticeDays: 60,
		Terms: domain.Terms{
			RentMinor: 27_500_00, Cycle: domain.Monthly, DueDay: 5,
			DepositMinor: 82_500_00, DepositHeldBy: domain.HeldByOwner,
		},
		Parties: []domain.Party{{
			PartyID: "11111111-2222-3333-4444-555555555555",
			Role:    domain.RoleTenant, Name: "Ravi Menon", Phone: "+919876543210",
		}},
	}
	if facts != nil {
		iv, err := effective.Since(facts.From)
		if err != nil {
			t.Fatalf("facts interval: %v", err)
		}
		h, err := tds.NewHistory([]effective.Record[tds.Facts]{
			{ID: "1", Range: iv, Kind: effective.KindChange, Value: *facts},
		})
		if err != nil {
			t.Fatalf("facts: %v", err)
		}
		d.Tax = h
	}
	return d
}

func resident(from effective.Date) *tds.Facts {
	return &tds.Facts{
		Deductor: tds.Business, Residency: tds.Resident,
		From: from, Source: "tenant declaration",
	}
}

func token() string { return time.Now().Format("150405.000000") }

// The story's primary scenario, end to end against the database: a lease
// created, its parties, rent and tax facts written in one transaction, and then
// activated.
func TestCreatingALeaseWritesEverythingInOneTransaction(t *testing.T) {
	req, plat := pools(t)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	property, unitID := unit(t, plat, "L-"+token())
	from := effective.Day(2029, 8, 5)

	s := store.New(req)
	created, err := s.Create(ctx, draft(t, property, unitID, from, effective.Day(2030, 8, 5), resident(from)))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if created.ID == "" || created.Event != domain.EventCreated {
		t.Fatalf("created %+v", created)
	}
	if created.Lease.State != domain.StateDraft {
		t.Errorf("a new lease is %s, want draft", created.Lease.State)
	}

	terms, err := s.Terms(ctx, created.ID, from)
	if err != nil {
		t.Fatalf("reading the terms back: %v", err)
	}
	if terms.RentMinor != 27_500_00 || terms.DueDay != 5 {
		t.Errorf("the rent came back as %+v", terms)
	}

	// Out for signature, then live. The tax facts are already recorded, so the
	// deferred trigger has nothing to object to.
	if err := send(ctx, req, created.ID); err != nil {
		t.Fatalf("sending for signature: %v", err)
	}
	if err := s.Activate(ctx, created.ID, domain.ActorOwner); err != nil {
		t.Fatalf("activating: %v", err)
	}
}

// The story's failure scenario. The second tenancy is refused, and the refusal
// names the lease that blocked it — "it conflicts with something" is not an
// answer anybody can act on.
func TestASecondTenancyOverTheSameDaysIsRefusedAndNamesTheFirst(t *testing.T) {
	req, plat := pools(t)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	property, unitID := unit(t, plat, "D-"+token())
	from := effective.Day(2029, 8, 5)
	s := store.New(req)

	first, err := s.Create(ctx, draft(t, property, unitID, from, effective.Day(2030, 8, 5), resident(from)))
	if err != nil {
		t.Fatalf("creating the first: %v", err)
	}
	if err := send(ctx, req, first.ID); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if err := s.Activate(ctx, first.ID, domain.ActorOwner); err != nil {
		t.Fatalf("activating the first: %v", err)
	}

	// Overlapping, and a legitimate draft — two competing offers on one flat are
	// allowed right up until one of them becomes a tenancy.
	overlap := effective.Day(2030, 1, 1)
	second, err := s.Create(ctx, draft(t, property, unitID, overlap, effective.Day(2031, 1, 1), resident(overlap)))
	if err != nil {
		t.Fatalf("a competing draft was refused: %v — a draft is an offer, not a letting", err)
	}
	if err := send(ctx, req, second.ID); err != nil {
		t.Fatalf("sending the second: %v", err)
	}

	err = s.Activate(ctx, second.ID, domain.ActorOwner)
	if !errors.Is(err, store.ErrDoubleLet) {
		t.Fatalf("one flat was let twice over the same days: %v", err)
	}
	if !strings.Contains(err.Error(), first.ID) {
		t.Errorf("the refusal does not name the conflicting lease: %v", err)
	}
}

// The story's second edge case, at the database rather than in Go: no tax facts,
// no tenancy.
func TestATenancyWithNoTaxFactsCannotBeActivated(t *testing.T) {
	req, plat := pools(t)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	property, unitID := unit(t, plat, "T-"+token())
	from := effective.Day(2029, 8, 5)
	s := store.New(req)

	created, err := s.Create(ctx, draft(t, property, unitID, from, effective.Day(2030, 8, 5), nil))
	if err != nil {
		t.Fatalf("a draft with no tax facts was refused: %v — a draft may be incomplete", err)
	}
	if err := send(ctx, req, created.ID); err != nil {
		t.Fatalf("sending: %v", err)
	}

	err = s.Activate(ctx, created.ID, domain.ActorOwner)
	if err == nil {
		t.Fatal("a tenancy started with no TDS section governing it")
	}
	if !strings.Contains(err.Error(), "TDS section") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// A section 195 tenancy needs the acknowledgement, and the database is the half
// that holds for a write that never went through Go.
func TestASection195TenancyNeedsItsAcknowledgementInTheDatabase(t *testing.T) {
	req, plat := pools(t)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	from := effective.Day(2029, 8, 5)
	s := store.New(req)

	nri := func(ack bool) *tds.Facts {
		f := &tds.Facts{
			Deductor: tds.Business, Residency: tds.NonResident,
			From: from, Source: "landlord declaration",
		}
		if ack {
			f.AcknowledgedOn, f.AcknowledgedBy = effective.Day(2029, 8, 1), "tenant:ravi"
		}
		return f
	}

	property, unitID := unit(t, plat, "N-"+token())
	unacknowledged, err := s.Create(ctx, draft(t, property, unitID, from, effective.Day(2030, 8, 5), nri(false)))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if err := send(ctx, req, unacknowledged.ID); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if err := s.Activate(ctx, unacknowledged.ID, domain.ActorOwner); err == nil {
		t.Fatal("a section 195 tenancy started unacknowledged")
	} else if !strings.Contains(err.Error(), "acknowledged") {
		t.Errorf("refused for the wrong reason: %v", err)
	}

	property, unitID = unit(t, plat, "A-"+token())
	acknowledged, err := s.Create(ctx, draft(t, property, unitID, from, effective.Day(2030, 8, 5), nri(true)))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if err := send(ctx, req, acknowledged.ID); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if err := s.Activate(ctx, acknowledged.ID, domain.ActorOwner); err != nil {
		t.Errorf("an acknowledged section 195 tenancy was refused: %v", err)
	}
}

// send moves a draft out for signature, which is the state activation comes
// from. It belongs to the document flow rather than to this story, so it is a
// helper here rather than a method.
func send(ctx context.Context, p tenancy.Pool, id string) error {
	return tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE leases SET state = 'pending_signature' WHERE id = $1`, id)
		return err
	})
}
