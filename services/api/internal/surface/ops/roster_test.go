package ops_test

import (
	"net/http"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Collections has two tabs: who owes, and the whole roster. Both read the same
// list until there is a list of the whole roster to read, and a firm whose
// tenancies are all square sees an empty screen saying it manages nothing
// (#313).
func TestOpsRosterCarriesTenanciesThatOweNothing(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/tenancies")
	if w.Code != http.StatusOK {
		t.Fatalf("GET tenancies: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Tenancies []struct {
			LeaseID   string `json:"lease_id"`
			RentMinor int64  `json:"rent_amount_minor"`
			DueMinor  int64  `json:"due_amount_minor"`
		} `json:"tenancies"`
	}
	decode(t, w, &out)

	arrears := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/arrears")
	var owed struct {
		Arrears []struct {
			LeaseID string `json:"lease_id"`
		} `json:"arrears"`
	}
	decode(t, arrears, &owed)

	if len(out.Tenancies) <= len(owed.Arrears) {
		t.Fatalf("roster has %d tenancies and %d owe money — the roster is the arrears list",
			len(out.Tenancies), len(owed.Arrears))
	}
	square, found := 0, false
	for _, tn := range out.Tenancies {
		if tn.DueMinor <= 0 {
			square++
		}
		if tn.LeaseID == isolationtest.LeasePriyaOwner {
			found = true
			if tn.RentMinor != 2500000 {
				t.Errorf("rent %d on the roster, want 2500000", tn.RentMinor)
			}
		}
	}
	if !found {
		t.Error("a known tenancy is missing from its own firm's roster")
	}
	if square == 0 {
		t.Error("every tenancy on the roster owes money, so a settled one is still missing")
	}
}

// The roster is one organisation's, like every other read here.
func TestOpsRosterLeavesOutAnotherOrganisationsTenancy(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/tenancies")
	var out struct {
		Tenancies []struct {
			LeaseID string `json:"lease_id"`
		} `json:"tenancies"`
	}
	decode(t, w, &out)

	for _, tn := range out.Tenancies {
		if tn.LeaseID == isolationtest.LeasePriyaOther {
			t.Fatalf("another firm's tenancy %s is on the roster", tn.LeaseID)
		}
	}
}
