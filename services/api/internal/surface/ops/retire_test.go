package ops_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Taking things off the book (#356). The manager's own mistakes have to be
// undoable, and the guard against stranding a tenant has to arrive as
// something they can read.

func TestRetiringABedTakesItOffTheBoard(t *testing.T) {
	mux := serve(t)
	label := bedLabel("R-RETIRE")

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/units/"+isolationtest.UnitSibling+"/beds",
		`{"label":"`+label+`","rent_amount_minor":500000}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("adding the bed: %d %s", w.Code, w.Body.String())
	}
	var made struct {
		Bed struct {
			ID string `json:"id"`
		} `json:"bed"`
	}
	decode(t, w, &made)

	w = call(t, mux, isolationtest.OrgOwner, http.MethodDelete, "/v1/ops/beds/"+made.Bed.ID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("retiring the bed: %d %s", w.Code, w.Body.String())
	}

	w = call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/beds")
	var board bedList
	decode(t, w, &board)
	for _, b := range board.Beds {
		if b.ID == made.Bed.ID {
			t.Fatal("a retired bed is still on the board")
		}
	}
}

func TestRetiringAHomeSomebodyLivesInIsRefusedInWords(t *testing.T) {
	mux, plat := serveWithPool(t)
	unit := seedLetFlat(t, plat)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodDelete, "/v1/ops/units/"+unit)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("retiring a let flat = %d %s, want 422 with the reason", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "living in") {
		t.Fatalf("the refusal says %s — the manager needs the reason, not a code", w.Body.String())
	}
}

// seedLetFlat is a flat of its own with a live tenancy on it: the suite
// commits, and retiring a fixture flat would take it away from every other
// test in the package.
func seedLetFlat(t *testing.T, plat tenancy.PlatformPool) string {
	t.Helper()
	tok := token(t)
	unitID, lease := uuidFrom(tok, "8"), uuidFrom(tok, "9")
	err := tenancy.Platform(context.Background(), plat, "seeding a let flat",
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, floor, carpet_area_sqft)
				VALUES ($1, $2, $3, 'flat', $4, 3, 615.00)`,
				unitID, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted,
				"R"+tok[:5]); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO leases (id, tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES ($1, $2, $3, $4, 'active', '2026-01-01'::date, '2027-01-01'::date)`,
				lease, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted,
				unitID); err != nil {
				return err
			}
			return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgOwner.String(),
				lease, "2026-01-01")
		})
	if err != nil {
		t.Fatalf("seeding a let flat: %v", err)
	}
	return unitID
}

func TestRetiringSomethingThatIsNotThereIs404(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodDelete,
		"/v1/ops/beds/00000000-0000-0000-0000-0000000000ff")
	if w.Code != http.StatusNotFound {
		t.Fatalf("retiring a bed that does not exist = %d %s, want 404", w.Code, w.Body.String())
	}
}
