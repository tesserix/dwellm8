package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	propertydomain "github.com/tesserix/dwellm8/services/api/internal/property/domain"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
	propertystore "github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// Ownership recorded after the fact (#302). Every property registered before
// onboarding wrote ownership is unbillable — the billing run has nobody to
// credit the rent to — and ownership genuinely changes anyway, when a property
// is sold. Both are the same write, asked by a session entitled to make it.

// registerUnowned puts a property in the owner's books the way the ops surface
// did before #302: registered, with nobody recorded as owning it.
func registerUnowned(t *testing.T, ownerOrgID, code string) string {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)
	ctx := tenancy.With(context.Background(), tenancy.ID(ownerOrgID))
	id, err := propertyservice.New(propertystore.New(pool)).Register(ctx, propertydomain.PropertyDraft{
		Code: code, Name: "Unowned " + code, Kind: "building",
		AddressLine1: "12 Residency Road", Locality: "Ashok Nagar",
		City: "Bengaluru", District: "Bengaluru Urban", StateCode: "KA", Pin: "560025",
	})
	if err != nil {
		t.Fatalf("registering the unowned property: %v", err)
	}
	return id
}

// putUnderGrant is the firm asking as it actually asks: under the mandate it
// holds, which is what makes the owner's property visible to it at all.
func putUnderGrant(t *testing.T, mux *http.ServeMux, org tenancy.ID, grant, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	ctx := tenancy.WithGrant(tenancy.With(r.Context(), org), tenancy.GrantID(grant))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(ctx))
	return w
}

func TestRecordingOwnershipOfAnAlreadyRegisteredProperty(t *testing.T) {
	mux := serveOnboarding(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"name": "Meera Sharma", "phone": uniquePhone(t)},
		"property": fullProperty("RSD", "Raheja Sunrise"),
		"units":    []map[string]any{{"code": "101", "kind": "flat", "carpet_area_sqft": 980}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("onboarding: %d %s", w.Code, w.Body.String())
	}
	var owner struct {
		OwnerOrgID   string `json:"owner_org_id"`
		OwnerPartyID string `json:"owner_party_id"`
		GrantID      string `json:"grant_id"`
	}
	decode(t, w, &owner)

	property := registerUnowned(t, owner.OwnerOrgID, fmt.Sprintf("RSD-2-%06d", rand.IntN(1000000)))

	t.Run("names who the rent is credited to", func(t *testing.T) {
		w := putUnderGrant(t, mux, isolationtest.OrgFirm, owner.GrantID,
			"/v1/ops/properties/"+property+"/ownership", map[string]any{"from": "2026-04-01"})
		if w.Code != http.StatusOK {
			t.Fatalf("recording ownership: %d %s", w.Code, w.Body.String())
		}
		var got struct {
			PropertyID   string `json:"property_id"`
			OwnerPartyID string `json:"owner_party_id"`
			From         string `json:"from"`
		}
		decode(t, w, &got)
		if got.PropertyID != property {
			t.Errorf("ownership recorded on %q, want %q", got.PropertyID, property)
		}
		if got.OwnerPartyID != owner.OwnerPartyID {
			t.Errorf("rent would be credited to %q, want the owner %q", got.OwnerPartyID, owner.OwnerPartyID)
		}
		if got.From != "2026-04-01" {
			t.Errorf("held from %q, want 2026-04-01", got.From)
		}
	})

	t.Run("recording it twice does not split the property in two", func(t *testing.T) {
		w := putUnderGrant(t, mux, isolationtest.OrgFirm, owner.GrantID,
			"/v1/ops/properties/"+property+"/ownership", map[string]any{"from": "2026-04-01"})
		if w.Code != http.StatusOK {
			t.Fatalf("recording ownership again: %d %s", w.Code, w.Body.String())
		}
	})
}

func TestRecordingOwnershipOfAPropertyTheFirmCannotSee(t *testing.T) {
	mux := serveOnboarding(t)

	w := put(t, mux, isolationtest.OrgFirm,
		"/v1/ops/properties/b1111111-0000-0000-0000-0000000000ff/ownership", map[string]any{})
	if w.Code != http.StatusNotFound {
		t.Fatalf("recording ownership of an unknown property: %d %s", w.Code, w.Body.String())
	}
}
