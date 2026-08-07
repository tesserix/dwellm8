package ops_test

import (
	"net/http"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The list is who to chase, in the order to chase them. Ordered by identifier
// and cut off at a page, a firm's real arrears sat below the fold (#306).
func TestOpsArrearsLeadsWithTheLargestDebt(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/arrears")
	if w.Code != http.StatusOK {
		t.Fatalf("GET arrears: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Arrears []struct {
			LeaseID  string `json:"lease_id"`
			DueMinor int64  `json:"due_amount_minor"`
		} `json:"arrears"`
	}
	decode(t, w, &out)

	if len(out.Arrears) == 0 {
		t.Fatal("no arrears at all, and the fixture has two unpaid invoices")
	}
	last := out.Arrears[0].DueMinor
	for i, a := range out.Arrears {
		if a.DueMinor <= 0 {
			t.Errorf("row %d (lease %s) owes %d — a settled tenancy is not an arrear",
				i, a.LeaseID, a.DueMinor)
		}
		if a.DueMinor > last {
			t.Fatalf("row %d owes %d after a row owing %d — not ordered by what is owed",
				i, a.DueMinor, last)
		}
		last = a.DueMinor
	}
}

// One tenancy's position, without downloading the firm's whole roster to find
// it — which is what the receipt and collection screens were doing (#306).
func TestOpsPositionReadsOneTenancy(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/tenancies/"+isolationtest.LeasePriyaOwner+"/position")
	if w.Code != http.StatusOK {
		t.Fatalf("GET position: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		LeaseID   string `json:"lease_id"`
		RentMinor int64  `json:"rent_amount_minor"`
		DueMinor  int64  `json:"due_amount_minor"`
	}
	decode(t, w, &out)

	if out.LeaseID != isolationtest.LeasePriyaOwner {
		t.Errorf("lease_id = %q, want the tenancy asked for", out.LeaseID)
	}
	if out.RentMinor != 2500000 || out.DueMinor != 2500000 {
		t.Errorf("rent %d due %d, want 2500000 and 2500000 — one unpaid invoice", out.RentMinor, out.DueMinor)
	}
}

// Another organisation's tenancy is a 404, not a position.
func TestOpsPositionRefusesAnotherOrganisationsTenancy(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/tenancies/"+isolationtest.LeasePriyaOther+"/position")
	if w.Code != http.StatusNotFound {
		t.Fatalf("reading another firm's tenancy: %d %s, want 404", w.Code, w.Body.String())
	}
}
