package ops_test

import (
	"net/http"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// What is about to happen, per property (#337). The fixture's two tenancies run
// to the end of the year on rent due on the 5th, and each carries one unpaid
// invoice — so the same book answers all three kinds.

type reminder struct {
	Kind        string `json:"kind"`
	LeaseID     string `json:"lease_id"`
	Property    string `json:"property"`
	PropertyID  string `json:"property_id"`
	Unit        string `json:"unit"`
	Locality    string `json:"locality"`
	On          string `json:"on"`
	DaysAway    int    `json:"days_away"`
	AmountMinor int64  `json:"amount_minor"`
	InsideWin   bool   `json:"inside_notice_window,omitempty"`
}

func reminders(t *testing.T, mux *http.ServeMux, org tenancy.ID, query string) []reminder {
	t.Helper()
	w := call(t, mux, org, http.MethodGet, "/v1/ops/reminders"+query)
	if w.Code != http.StatusOK {
		t.Fatalf("GET reminders: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Reminders []reminder `json:"reminders"`
	}
	decode(t, w, &out)
	return out.Reminders
}

func TestOpsRemindersCarryTheRentAboutToFallDue(t *testing.T) {
	mux := serve(t)

	// Forty-five days, so a monthly rent falls inside the window whatever day
	// of the month the suite runs on.
	got := reminders(t, mux, isolationtest.OrgOwner, "?days=45")

	var due []reminder
	for _, r := range got {
		if r.Kind == "rent_due" {
			due = append(due, r)
		}
	}
	if len(due) == 0 {
		t.Fatal("nothing falls due in forty-five days, and the fixture has two live tenancies on monthly rent")
	}
	for _, r := range due {
		if r.AmountMinor <= 0 {
			t.Errorf("lease %s falls due for %d", r.LeaseID, r.AmountMinor)
		}
		if r.On == "" || r.DaysAway < 0 {
			t.Errorf("lease %s falls due %q, %d days away", r.LeaseID, r.On, r.DaysAway)
		}
		if r.Property == "" || r.Unit == "" {
			t.Errorf("a reminder names where it is: property %q unit %q", r.Property, r.Unit)
		}
	}
}

func TestOpsRemindersCarryWhatIsAlreadyOverdue(t *testing.T) {
	mux := serve(t)

	got := reminders(t, mux, isolationtest.OrgOwner, "?days=45")

	overdue := 0
	for _, r := range got {
		if r.Kind != "rent_overdue" {
			continue
		}
		overdue++
		if r.AmountMinor <= 0 {
			t.Errorf("lease %s is overdue for %d — a settled tenancy is not a reminder", r.LeaseID, r.AmountMinor)
		}
	}
	if overdue == 0 {
		t.Fatal("nothing is overdue, and the fixture has two unpaid invoices")
	}
}

func TestOpsRemindersCarryTenanciesRunningOut(t *testing.T) {
	mux := serve(t)

	// The fixture's terms end on the last day of the year, so the window has to
	// reach that far to see them.
	got := reminders(t, mux, isolationtest.OrgOwner, "?days=365")

	ending := 0
	for _, r := range got {
		if r.Kind == "tenancy_ending" {
			ending++
			if r.On == "" {
				t.Errorf("lease %s ends on no date", r.LeaseID)
			}
		}
	}
	if ending == 0 {
		t.Fatal("no tenancy is running out within a year, and the fixture's terms end in December")
	}
}

// Soonest first: a list a manager reads top down is a list in the order things
// happen.
func TestOpsRemindersAreSoonestFirst(t *testing.T) {
	mux := serve(t)

	got := reminders(t, mux, isolationtest.OrgOwner, "?days=365")
	if len(got) < 2 {
		t.Fatalf("only %d reminders — not enough to have an order", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].On < got[i-1].On {
			t.Fatalf("%s (%s) comes after %s (%s)", got[i].On, got[i].Kind, got[i-1].On, got[i-1].Kind)
		}
	}
}

// The reminder list is the firm's own book. Another organisation's tenancies
// are not on it, and RLS is what keeps them off.
func TestOpsRemindersStopAtTheOrganisation(t *testing.T) {
	mux := serve(t)

	mine := reminders(t, mux, isolationtest.OrgOwner, "?days=365")
	theirs := reminders(t, mux, isolationtest.OrgOutsider, "?days=365")
	for _, a := range mine {
		for _, b := range theirs {
			if a.LeaseID == b.LeaseID {
				t.Fatalf("tenancy %s is reminded to both organisations", a.LeaseID)
			}
		}
	}
}
