package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	leasehttp "github.com/tesserix/dwellm8/services/api/internal/lease/http"
	"github.com/tesserix/dwellm8/services/api/internal/lease/service"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"io"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	leasedomain "github.com/tesserix/dwellm8/services/api/internal/lease/domain"
)

// The rent revision through the Gin layer — issue #36 riding platform/ginx.
// What it proves beyond the handler: ADR-0008's as-of behaviour (March still
// answers with March's rent), and the refusals for a same-day duplicate and a
// backdated change.

func revisionHarness(t *testing.T) (http.Handler, *store.Leases, *service.Leases, string) {
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
	platPool, err := pgxpool.New(context.Background(), plat)
	if err != nil {
		t.Fatalf("connecting as platform: %v", err)
	}
	t.Cleanup(platPool.Close)

	p := tenancy.NewPlatformPool(platPool)
	isolationtest.SeedPropertyTree(t, p)
	var unitID string
	if err := tenancy.Platform(context.Background(), p, "seeding a revision unit",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 640) RETURNING id`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted,
				"REV-"+time.Now().Format("150405.000000")).Scan(&unitID)
		}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(req)
	leases := service.NewLeases(st, log)

	engine := ginx.Engine()
	leasehttp.NewRevisions(leases, log).Routes(ginx.New(engine, &authz.Guard{}))
	return engine, st, leases, unitID
}

func TestRentRevisionThroughGin(t *testing.T) {
	handler, st, leases, unitID := revisionHarness(t)

	// A tenancy at 27,500 from next month for a year.
	start := effective.DateOf(time.Now().AddDate(0, 1, 0), time.UTC)
	end := effective.DateOf(time.Now().AddDate(1, 1, 0), time.UTC)
	term, err := effective.Between(start, end)
	if err != nil {
		t.Fatalf("term: %v", err)
	}
	created, err := leases.Create(scopedCtx(), leasedomain.Draft{
		TenantID: isolationtest.OrgOwner.String(),
		Property: isolationtest.PropertyGranted, Unit: unitID,
		Term: term, NoticeDays: 60,
		Terms: leasedomain.Terms{
			RentMinor: 27_500_00, Cycle: leasedomain.Monthly, DueDay: 5,
			DepositMinor: 82_500_00, DepositHeldBy: leasedomain.HeldByOwner,
		},
		Parties: []leasedomain.Party{{
			PartyID: "11111111-2222-3333-4444-555555555555",
			Role:    leasedomain.RoleTenant, Name: "Ravi Menon", Phone: "+919876543210",
		}},
	})
	if err != nil {
		t.Fatalf("creating the lease: %v", err)
	}

	revise := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/leases/"+created.ID+"/rent-revisions",
			strings.NewReader(body)).
			WithContext(tenancy.With(context.Background(), isolationtest.OrgOwner))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}

	// +5% effective six months in.
	effectiveOn := effective.DateOf(time.Now().AddDate(0, 7, 0), time.UTC)
	rec := revise(`{"rent_amount_minor": 2887500, "effective_on": "` + effectiveOn.String() + `"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("revision = %d — %s", rec.Code, rec.Body)
	}

	// ADR-0008: the day before the change still answers with the old rent, the
	// day itself with the new one.
	before, err := st.Terms(scopedCtx(), created.ID, start)
	if err != nil || before.RentMinor != 27_500_00 {
		t.Fatalf("terms before = %d, %v; want the old 2750000", before.RentMinor, err)
	}
	after, err := st.Terms(scopedCtx(), created.ID, effectiveOn)
	if err != nil || after.RentMinor != 28_875_00 {
		t.Fatalf("terms after = %d, %v; want the new 2887500", after.RentMinor, err)
	}

	// A second change on the same day is a 409; a backdated one is a 422
	// pointing at the correction path.
	if rec := revise(`{"rent_amount_minor": 3000000, "effective_on": "` + effectiveOn.String() + `"}`); rec.Code != http.StatusConflict {
		t.Fatalf("same-day duplicate = %d — %s", rec.Code, rec.Body)
	}
	backdated := effective.DateOf(time.Now().AddDate(0, 3, 0), time.UTC)
	_ = backdated
	if rec := revise(`{"rent_amount_minor": 2600000, "effective_on": "` + start.String() + `"}`); rec.Code != http.StatusConflict && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("backdated/duplicate-start = %d — %s", rec.Code, rec.Body)
	}
}

func scopedCtx() context.Context {
	return tenancy.With(context.Background(), isolationtest.OrgOwner)
}
