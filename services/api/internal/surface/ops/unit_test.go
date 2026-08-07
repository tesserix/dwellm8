package ops_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The flat itself, opened from the property record (#338). Until this existed
// a manager could read a unit's code and nothing else — not its size, not what
// it is let at, not the meter number they were standing in front of.

type unitRecord struct {
	Unit struct {
		ID                    string  `json:"id"`
		Code                  string  `json:"code"`
		Kind                  string  `json:"kind"`
		Floor                 int     `json:"floor"`
		Occupancy             string  `json:"occupancy"`
		CarpetAreaSqft        float64 `json:"carpet_area_sqft"`
		BuiltupAreaSqft       float64 `json:"builtup_area_sqft"`
		ShareCertificateNo    string  `json:"share_certificate_no"`
		ElectricityConsumerNo string  `json:"electricity_consumer_no"`
		WaterConnectionNo     string  `json:"water_connection_no"`
	} `json:"unit"`
	Property struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Locality string `json:"locality"`
	} `json:"property"`
	Tenancy *struct {
		LeaseID   string `json:"lease_id"`
		Tenant    string `json:"tenant"`
		RentMinor int64  `json:"rent_amount_minor"`
		DueMinor  int64  `json:"due_amount_minor"`
		Ends      string `json:"ends"`
		LetFrom   string `json:"let_from"`
	} `json:"tenancy"`
	Listing *struct {
		ID        string  `json:"id"`
		State     string  `json:"state"`
		Bedrooms  int     `json:"bedrooms"`
		RentMinor int64   `json:"rent_amount_minor"`
		Carpet    float64 `json:"carpet_area_sqft"`
	} `json:"listing"`
	Ancillaries []struct {
		Code string `json:"code"`
		Kind string `json:"kind"`
	} `json:"ancillaries"`
}

func TestOpsUnitRecordCarriesTheFlatAndWhoIsInIt(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/units/"+isolationtest.UnitGrantedA)
	if w.Code != http.StatusOK {
		t.Fatalf("GET the unit: %d %s", w.Code, w.Body.String())
	}
	var out unitRecord
	decode(t, w, &out)

	if out.Unit.ID != isolationtest.UnitGrantedA || out.Unit.Code != "101" {
		t.Fatalf("the unit asked for is the one returned, got %+v", out.Unit)
	}
	if out.Unit.CarpetAreaSqft != 620 || out.Unit.BuiltupAreaSqft != 890 {
		t.Errorf("the flat's size comes from the register, got carpet %v builtup %v",
			out.Unit.CarpetAreaSqft, out.Unit.BuiltupAreaSqft)
	}
	if out.Unit.Floor != 1 || out.Unit.Kind != "flat" {
		t.Errorf("floor and kind come from the register, got %+v", out.Unit)
	}
	if out.Property.ID != isolationtest.PropertyGranted || out.Property.Name == "" {
		t.Errorf("the unit names the building it is in, got %+v", out.Property)
	}
	if out.Tenancy == nil {
		t.Fatal("101 is let in the fixture and the record must say so")
	}
	if out.Tenancy.Tenant == "" || out.Tenancy.RentMinor <= 0 {
		t.Errorf("a let flat names its tenant and its rent, got %+v", out.Tenancy)
	}
	if out.Tenancy.DueMinor <= 0 {
		t.Errorf("the fixture leaves one invoice unpaid, got due %d", out.Tenancy.DueMinor)
	}
}

// The parking slot allotted to 101 is not a lettable unit and never appears on
// the property's unit list — but it belongs to this flat, and the manager
// handing over keys needs to know it exists.
func TestOpsUnitRecordListsWhatIsAllottedToTheFlat(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/units/"+isolationtest.UnitGrantedA)
	if w.Code != http.StatusOK {
		t.Fatalf("GET the unit: %d %s", w.Code, w.Body.String())
	}
	var out unitRecord
	decode(t, w, &out)

	for _, a := range out.Ancillaries {
		if a.Code == "P-1" && a.Kind == "parking" {
			return
		}
	}
	t.Fatalf("P-1 is allotted to 101 in the fixture, got %+v", out.Ancillaries)
}

func TestOpsUnitRecordShowsAnEmptyFlatAsEmpty(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/units/"+isolationtest.UnitSibling)
	if w.Code != http.StatusOK {
		t.Fatalf("GET the unit: %d %s", w.Code, w.Body.String())
	}
	var out unitRecord
	decode(t, w, &out)

	if out.Unit.Code != "103" {
		t.Fatalf("wrong unit, got %+v", out.Unit)
	}
	if out.Tenancy != nil {
		t.Errorf("103 has no tenancy in the fixture, got %+v", out.Tenancy)
	}
}

// Bedrooms are not on the register — the schema models rooms as units, not as
// a count on the flat — so BHK is what the advert says. Reading it from the
// listing is the only honest answer this surface can give (#338).
func TestOpsUnitRecordReadsTheBedroomCountFromItsAdvert(t *testing.T) {
	mux, plat := serveWithPool(t)
	seedListingOnUnit(t, plat, isolationtest.UnitSibling, 2, 3300000)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/units/"+isolationtest.UnitSibling)
	if w.Code != http.StatusOK {
		t.Fatalf("GET the unit: %d %s", w.Code, w.Body.String())
	}
	var out unitRecord
	decode(t, w, &out)

	if out.Listing == nil {
		t.Fatal("103 is advertised and the record must carry the advert")
	}
	if out.Listing.Bedrooms != 2 {
		t.Errorf("the advert says 2 BHK, got %d", out.Listing.Bedrooms)
	}
	if out.Listing.RentMinor != 3300000 {
		t.Errorf("the advert's asking rent, got %d", out.Listing.RentMinor)
	}
}

// Not theirs and not there read the same, as everywhere else on this surface.
func TestOpsCannotOpenAnotherOrganisationsUnit(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/units/"+isolationtest.UnitSecond)
	if w.Code != http.StatusNotFound {
		t.Fatalf("opening the second landlord's flat: %d %s, want 404", w.Code, w.Body.String())
	}
}

// seedListingOnUnit advertises a unit. Live and published, because a draft
// nobody published is not what the manager is asking about.
func seedListingOnUnit(t *testing.T, plat tenancy.PlatformPool, unitID string, bedrooms int, rentMinor int64) {
	t.Helper()
	err := tenancy.Platform(context.Background(), plat, "advertising a unit for the unit record",
		func(ctx context.Context, tx pgx.Tx) error {
			// Upserted, not deleted and rewritten: the schema keeps listings for
			// good, and one live advert per unit is a database rule this suite's
			// commits would otherwise break on the second run.
			_, err := tx.Exec(ctx, `
				INSERT INTO listings (tenant_id, property_id, unit_id, state, published_at,
				                      headline, locality, city, state_code,
				                      rent_minor, bedrooms, carpet_area_sqft, costs_confirmed)
				VALUES ($1, $2, $3, 'live', now(), 'Two rooms on the first floor',
				        'Indiranagar', 'Bengaluru', 'KA', $4, $5, 660.00, true)
				ON CONFLICT (tenant_id, unit_id) WHERE state IN ('live', 'paused')
				DO UPDATE SET rent_minor = excluded.rent_minor, bedrooms = excluded.bedrooms`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unitID,
				rentMinor, bedrooms)
			return err
		})
	if err != nil {
		t.Fatalf("advertising a unit: %v", err)
	}
}
