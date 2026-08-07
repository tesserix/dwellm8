package store_test

import (
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
)

// The arrears screen is the whole organisation's, not the first page of it: a
// roster read lease by lease and cut off at a limit reports the tenancies with
// low identifiers, not the ones that owe (#306).
func TestOutstandingRanksEveryTenancyThatOwesByWhatItOwes(t *testing.T) {
	small, large := newFixture(t), newFixture(t)

	small.post(domain.Invoice(1_000_000, 0, small.place, small.tenant, small.owner,
		small.src(domain.SourceLeaseCharge, "invoice", "2026-03-01")))
	large.post(domain.Invoice(4_000_000, 0, large.place, large.tenant, large.owner,
		large.src(domain.SourceLeaseCharge, "invoice", "2026-03-01")))
	// Half of the larger tenancy's rent, so it still owes the most.
	large.post(domain.Payment(1_000_000, 4_000_000, large.place, large.tenant,
		large.src("payment", "receipt", "2026-03-05")))

	rows, err := small.ledger.Outstanding(small.ctx, date("2026-03-31"))
	if err != nil {
		t.Fatalf("reading what is outstanding: %v", err)
	}

	due := map[string]domain.Minor{}
	rank := map[string]int{}
	var last domain.Minor = -1
	for i, r := range rows {
		due[r.LeaseID], rank[r.LeaseID] = r.Due, i
		if last >= 0 && r.Due > last {
			t.Fatalf("row %d owes %s after a row owing %s — not ordered by what is owed", i, r.Due, last)
		}
		last = r.Due
	}

	if got := due[small.lease]; got != 1_000_000 {
		t.Errorf("the smaller tenancy owes %s, want 10,000.00", got)
	}
	if got := due[large.lease]; got != 3_000_000 {
		t.Errorf("the larger tenancy owes %s, want 30,000.00", got)
	}
	if rank[large.lease] > rank[small.lease] {
		t.Errorf("the tenancy owing 30,000.00 is listed after one owing 10,000.00")
	}
}

// A tenancy that has paid is not an arrear, and putting it on the list makes a
// manager chase somebody who owes nothing.
func TestOutstandingLeavesOutATenancyThatOwesNothing(t *testing.T) {
	f := newFixture(t)
	f.post(domain.Invoice(2_000_000, 0, f.place, f.tenant, f.owner,
		f.src(domain.SourceLeaseCharge, "invoice", "2026-04-01")))
	f.post(domain.Payment(2_000_000, 2_000_000, f.place, f.tenant,
		f.src("payment", "receipt", "2026-04-02")))

	rows, err := f.ledger.Outstanding(f.ctx, date("2026-04-30"))
	if err != nil {
		t.Fatalf("reading what is outstanding: %v", err)
	}
	for _, r := range rows {
		if r.LeaseID == f.lease {
			t.Fatalf("a settled tenancy is on the arrears list owing %s", r.Due)
		}
	}
}
