package http_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	leasehttp "github.com/tesserix/dwellm8/services/api/internal/lease/http"
	"github.com/tesserix/dwellm8/services/api/internal/lease/service"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The lease routes against a real database, because the two refusals worth
// testing are the database's: one flat let twice, and a tenancy with no TDS
// section. A mocked store would assert that the mock returns what it was told to.

func handler(t *testing.T) (*http.ServeMux, string) {
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

	// A unit code unique per run, not per test name: units.code is unique per
	// property, so a name-derived code makes the suite pass once and fail on
	// every re-run — which is the shape of a test that gets deleted rather than
	// fixed.
	var unit string
	code := "H-" + time.Now().Format("150405.000")
	if err := tenancy.Platform(context.Background(), p, "seeding a unit for the handler test",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 600) RETURNING id`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, code).Scan(&unit)
		}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	leasehttp.NewLeases(service.NewLeases(store.New(req), log), log).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux, unit
}

// scoped puts the organisation in the request context, which is what the tenancy
// middleware does in the real server.
func scoped(r *http.Request) *http.Request {
	return r.WithContext(tenancy.With(r.Context(), isolationtest.OrgOwner))
}

func body(unit string, extra string) string {
	return `{
		"property_id": "` + isolationtest.PropertyGranted + `",
		"unit_id": "` + unit + `",
		"start_on": "2029-08-05", "end_on": "2030-08-05", "notice_days": 60,
		"rent_amount_minor": 2750000, "cycle": "monthly", "due_day": 5,
		"deposit_amount_minor": 8250000, "deposit_held_by": "owner",
		"parties": [{"party_id": "11111111-2222-3333-4444-555555555555",
		             "role": "tenant", "name": "Ravi Menon", "phone": "+919876543210"}]` +
		extra + `}`
}

const residentTax = `,
	"tax": {"deductor_class": "business", "landlord_residency": "resident",
	        "source": "tenant declaration"}`

func post(t *testing.T, mux *http.ServeMux, path, payload string) *httptest.ResponseRecorder {
	t.Helper()
	req := scoped(httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCreatingAndStartingATenancy(t *testing.T) {
	mux, unit := handler(t)

	rec := post(t, mux, "/v1/leases", body(unit, residentTax))
	if rec.Code != http.StatusCreated {
		t.Fatalf("creating answered %d: %s", rec.Code, rec.Body)
	}
	var created struct{ ID, State, Event string }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if created.State != "draft" || created.Event != "lease.created" {
		t.Errorf("created %+v, want a draft and lease.created", created)
	}

	// A draft is not a tenancy: it goes out for signature first, and the state
	// machine says so rather than the handler.
	rec = post(t, mux, "/v1/leases/"+created.ID+"/activate", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("activating a draft answered %d, want 404 — nothing is awaiting signature", rec.Code)
	}
}

// The organisation comes from the session. A body naming another one changes
// nothing, which is the property worth asserting.
func TestTheBodyCannotChooseItsOwnOrganisation(t *testing.T) {
	mux, unit := handler(t)

	// tenant_id is not a field, so sending it is a 400 rather than a silent
	// override — unknown fields are refused.
	payload := strings.Replace(body(unit, residentTax), `"property_id"`,
		`"tenant_id": "22222222-2222-2222-2222-222222222222", "property_id"`, 1)
	rec := post(t, mux, "/v1/leases", payload)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a body naming its own organisation answered %d, want 400", rec.Code)
	}

	// And with no organisation in the session at all, nothing is created.
	req := httptest.NewRequest(http.MethodPost, "/v1/leases", strings.NewReader(body(unit, residentTax)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("an unscoped request answered %d, want 401", rec.Code)
	}
}

// A request that cannot be a lease is named back to the caller. "Invalid
// request" is not something anybody can act on.
func TestAnImpossibleLeaseIsNamedBack(t *testing.T) {
	mux, unit := handler(t)

	for _, c := range []struct {
		name    string
		payload string
		want    int
	}{
		{"no tenant on it", strings.Replace(body(unit, residentTax),
			`"role": "tenant"`, `"role": "occupant"`, 1), http.StatusUnprocessableEntity},
		{"no rent", strings.Replace(body(unit, residentTax),
			`"rent_amount_minor": 2750000`, `"rent_amount_minor": 0`, 1), http.StatusUnprocessableEntity},
		{"a due day of the 32nd", strings.Replace(body(unit, residentTax),
			`"due_day": 5`, `"due_day": 32`, 1), http.StatusUnprocessableEntity},
		{"a start that is not a date", strings.Replace(body(unit, residentTax),
			`"start_on": "2029-08-05"`, `"start_on": "next Tuesday"`, 1), http.StatusBadRequest},
		{"a misspelled field", strings.Replace(body(unit, residentTax),
			`"rent_amount_minor"`, `"rentAmount"`, 1), http.StatusBadRequest},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := post(t, mux, "/v1/leases", c.payload)
			if rec.Code != c.want {
				t.Errorf("answered %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
			var e struct{ Error string }
			_ = json.Unmarshal(rec.Body.Bytes(), &e)
			if e.Error == "" {
				t.Error("the refusal says nothing the caller can act on")
			}
		})
	}
}

// The two refusals that matter, and they must not be flattened into one another:
// a missing TDS section is 422 because something about the lease is absent, and
// a double-let is 409 because the world disagrees with a request that is fine.
func TestTheTwoRefusalsAreDistinguishable(t *testing.T) {
	mux, unit := handler(t)

	// No tax facts: created, sent for signature, refused at activation.
	rec := post(t, mux, "/v1/leases", body(unit, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("a draft without tax facts was refused: %d %s", rec.Code, rec.Body)
	}
	var untaxed struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &untaxed)

	sendForSignature(t, untaxed.ID)
	rec = post(t, mux, "/v1/leases/"+untaxed.ID+"/activate", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a tenancy with no TDS section answered %d, want 422: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "TDS section") {
		t.Errorf("the refusal does not say what is missing: %s", rec.Body)
	}
}

func sendForSignature(t *testing.T, id string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	defer p.Close()
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	if err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE leases SET state = 'pending_signature' WHERE id = $1`, id)
		return err
	}); err != nil {
		t.Fatalf("sending for signature: %v", err)
	}
}
