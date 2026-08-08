package ops_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The managers a firm employs (#353). A firm that grows past one person has to
// be able to say who is responsible for which building, and how much any one
// person carries — a cap the app can forget to check is not a cap.

type teamPage struct {
	Roles []struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Permissions   []string `json:"permissions"`
		PropertyLimit int      `json:"property_limit"`
		People        int      `json:"people"`
	} `json:"roles"`
	Team []struct {
		ID            string `json:"id"`
		FullName      string `json:"full_name"`
		RoleName      string `json:"role_name"`
		Designation   string `json:"designation"`
		Phone         string `json:"phone"`
		PANMasked     string `json:"pan_masked"`
		SalaryMinor   int64  `json:"salary_minor"`
		PayFrequency  string `json:"pay_frequency"`
		JoinedOn      string `json:"joined_on"`
		State         string `json:"state"`
		PropertyLimit int    `json:"property_limit"`
		Held          int    `json:"held"`
	} `json:"team"`
	Assignments []struct {
		ID           string `json:"id"`
		StaffID      string `json:"staff_id"`
		PropertyID   string `json:"property_id"`
		PropertyName string `json:"property_name"`
	} `json:"assignments"`
}

func aStaffRole(t *testing.T, mux *http.ServeMux, name string, limit int) string {
	t.Helper()
	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/roles",
		fmt.Sprintf(`{"name":%q,"permissions":["property.read","maintenance.write"],"property_limit":%d}`,
			name, limit))
	if w.Code != http.StatusCreated {
		t.Fatalf("creating a role: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Role struct {
			ID string `json:"id"`
		} `json:"role"`
	}
	decode(t, w, &out)
	return out.Role.ID
}

func aStaffMember(t *testing.T, mux *http.ServeMux, role, name string) string {
	t.Helper()
	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff",
		fmt.Sprintf(`{"role_id":%q,"full_name":%q,"phone":"+919876500001",
		              "designation":"Field executive","employment_type":"full_time",
		              "joined_on":"2026-01-05","pan":"ABCDE1234F",
		              "salary_minor":4500000,"pay_frequency":"monthly","state":"active"}`, role, name))
	if w.Code != http.StatusCreated {
		t.Fatalf("employing a manager: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Member struct {
			ID string `json:"id"`
		} `json:"member"`
	}
	decode(t, w, &out)
	return out.Member.ID
}

// A second property under the same firm, so the cap has something to refuse.
func aSecondProperty(t *testing.T, plat tenancy.PlatformPool) string {
	t.Helper()
	id := uuid.NewString()
	code := "STF" + id[:5]
	err := tenancy.Platform(context.Background(), plat, "a second building for the team",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO properties (id, tenant_id, code, name, kind,
				                        address_line1, locality, city, state_code, pin)
				VALUES ($1::uuid, $2::uuid, $3::citext, $3::text, 'building',
				        '2 Road', 'Kaloor', 'Kochi', 'KL', '682001')`,
				id, isolationtest.OrgOwner.String(), code)
			return err
		})
	if err != nil {
		t.Fatalf("seeding a second property: %v", err)
	}
	return id
}

func teamOf(t *testing.T, mux *http.ServeMux) teamPage {
	t.Helper()
	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/staff")
	if w.Code != http.StatusOK {
		t.Fatalf("reading the team: %d %s", w.Code, w.Body.String())
	}
	var out teamPage
	decode(t, w, &out)
	return out
}

func TestAManagerIsOnboardedIntoTheFirmAndReadsBackWithTheirTerms(t *testing.T) {
	mux, _ := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 5)
	id := aStaffMember(t, mux, role, "Asha Nair")

	page := teamOf(t, mux)
	var got bool
	for _, m := range page.Team {
		if m.ID != id {
			continue
		}
		got = true
		if m.FullName != "Asha Nair" || m.Designation != "Field executive" {
			t.Errorf("the manager reads back as %+v", m)
		}
		if m.SalaryMinor != 4500000 || m.PayFrequency != "monthly" || m.JoinedOn != "2026-01-05" {
			t.Errorf("the terms read back as %+v", m)
		}
		if m.PropertyLimit != 5 || m.Held != 0 {
			t.Errorf("the load reads back as %d of %d", m.Held, m.PropertyLimit)
		}
	}
	if !got {
		t.Fatal("the manager the firm employed is not on the team")
	}
}

// ADR-0013. The number reaches the API because a firm has to type it once; what
// is kept, and what comes back, is the mask.
func TestThePANIsMaskedBeforeItIsKeptAndNeverComesBackWhole(t *testing.T) {
	mux, _ := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 5)
	id := aStaffMember(t, mux, role, "Asha Nair")

	page := teamOf(t, mux)
	for _, m := range page.Team {
		if m.ID != id {
			continue
		}
		if m.PANMasked != "XXXXXX234F" {
			t.Fatalf("the PAN is held as %q", m.PANMasked)
		}
	}
	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/staff")
	if strings.Contains(w.Body.String(), "ABCDE1234F") {
		t.Fatal("the whole PAN came back over the wire")
	}
}

func TestAPANThatIsNotAPANIsRefused(t *testing.T) {
	mux, _ := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 5)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff",
		fmt.Sprintf(`{"role_id":%q,"full_name":"Asha Nair","phone":"+919876500001","pan":"NOTAPAN"}`, role))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a malformed PAN was accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestAManagerWithNoNameOrNoWayToBeReachedIsRefused(t *testing.T) {
	mux, _ := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 5)

	for _, body := range []string{
		fmt.Sprintf(`{"role_id":%q,"full_name":"  ","phone":"+919876500001"}`, role),
		fmt.Sprintf(`{"role_id":%q,"full_name":"Asha Nair"}`, role),
		fmt.Sprintf(`{"role_id":%q,"full_name":"Asha Nair","phone":"+919876500001","salary_minor":-1}`, role),
	} {
		w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff", body)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s was accepted: %d %s", body, w.Code, w.Body.String())
		}
	}
}

func TestAManagerIsNotGivenMorePropertiesThanTheirRoleAllows(t *testing.T) {
	mux, plat := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 1)
	asha := aStaffMember(t, mux, role, "Asha Nair")

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+asha+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, aSecondProperty(t, plat)))
	if w.Code != http.StatusCreated {
		t.Fatalf("the first property: %d %s", w.Code, w.Body.String())
	}

	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+asha+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, aSecondProperty(t, plat)))
	if w.Code != http.StatusConflict {
		t.Fatalf("the property past the cap: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "as many properties") {
		t.Errorf("the refusal says %s, which does not tell a manager what to do", w.Body.String())
	}
}

func TestABuildingSomebodyElseHoldsIsNotHandedToASecondManager(t *testing.T) {
	mux, plat := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 5)
	asha := aStaffMember(t, mux, role, "Asha Nair")
	ravi := aStaffMember(t, mux, role, "Ravi Menon")
	block := aSecondProperty(t, plat)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+asha+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, block))
	if w.Code != http.StatusCreated {
		t.Fatalf("the first assignment: %d %s", w.Code, w.Body.String())
	}
	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+ravi+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, block))
	if w.Code != http.StatusConflict {
		t.Fatalf("a second manager on one building: %d %s", w.Code, w.Body.String())
	}
}

func TestHandingAPropertyBackFreesTheManagerToTakeAnother(t *testing.T) {
	mux, plat := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 1)
	asha := aStaffMember(t, mux, role, "Asha Nair")

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+asha+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, aSecondProperty(t, plat)))
	if w.Code != http.StatusCreated {
		t.Fatalf("the first property: %d %s", w.Code, w.Body.String())
	}
	var made struct {
		Assignment struct {
			ID string `json:"id"`
		} `json:"assignment"`
	}
	decode(t, w, &made)

	w = call(t, mux, isolationtest.OrgOwner, http.MethodDelete,
		"/v1/ops/staff/assignments/"+made.Assignment.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("handing it back: %d %s", w.Code, w.Body.String())
	}

	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+asha+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, aSecondProperty(t, plat)))
	if w.Code != http.StatusCreated {
		t.Fatalf("the next property after handing one back: %d %s", w.Code, w.Body.String())
	}
}

func TestTheFirmRaisesOnePersonsWorkloadAboveTheirRole(t *testing.T) {
	mux, plat := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 1)
	asha := aStaffMember(t, mux, role, "Asha Nair")

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+asha+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, aSecondProperty(t, plat)))
	if w.Code != http.StatusCreated {
		t.Fatalf("the first property: %d %s", w.Code, w.Body.String())
	}
	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPatch, "/v1/ops/staff/"+asha,
		`{"property_limit":2}`)
	if w.Code != http.StatusOK {
		t.Fatalf("raising the workload: %d %s", w.Code, w.Body.String())
	}
	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+asha+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, aSecondProperty(t, plat)))
	if w.Code != http.StatusCreated {
		t.Fatalf("the second property under a raised workload: %d %s", w.Code, w.Body.String())
	}
}

// Somebody who has left keeps their record and loses the buildings: an exit
// nobody hands the keys back on is how a property ends up unmanaged.
func TestSomebodyWhoLeavesIsDatedAndHandsBackWhatTheyHeld(t *testing.T) {
	mux, plat := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 5)
	asha := aStaffMember(t, mux, role, "Asha Nair")

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/"+asha+"/assignments",
		fmt.Sprintf(`{"property_id":%q}`, aSecondProperty(t, plat)))
	if w.Code != http.StatusCreated {
		t.Fatalf("the property: %d %s", w.Code, w.Body.String())
	}

	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPatch, "/v1/ops/staff/"+asha,
		`{"state":"exited","exited_on":"2026-08-01"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("recording the exit: %d %s", w.Code, w.Body.String())
	}

	page := teamOf(t, mux)
	for _, m := range page.Team {
		if m.ID == asha && (m.State != "exited" || m.Held != 0) {
			t.Fatalf("somebody who left reads back as %s holding %d", m.State, m.Held)
		}
	}
	for _, a := range page.Assignments {
		if a.StaffID == asha {
			t.Fatal("a manager who has left is still responsible for a building")
		}
	}
}

func TestTheWeeklyRotaIsSetAndReadBack(t *testing.T) {
	mux, _ := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 5)
	asha := aStaffMember(t, mux, role, "Asha Nair")

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPut, "/v1/ops/staff/"+asha+"/shifts",
		`{"shifts":[{"weekday":1,"starts_at":"09:00","ends_at":"18:00"},
		            {"weekday":6,"starts_at":"10:00","ends_at":"14:00"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("setting the rota: %d %s", w.Code, w.Body.String())
	}

	w = call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/staff/"+asha+"/shifts")
	if w.Code != http.StatusOK {
		t.Fatalf("reading the rota: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Shifts []struct {
			Weekday  int    `json:"weekday"`
			StartsAt string `json:"starts_at"`
			EndsAt   string `json:"ends_at"`
		} `json:"shifts"`
	}
	decode(t, w, &out)
	if len(out.Shifts) != 2 || out.Shifts[0].Weekday != 1 || out.Shifts[1].StartsAt != "10:00" {
		t.Fatalf("the week reads back as %+v", out.Shifts)
	}
}

func TestARotaThatIsNotAWeekIsRefused(t *testing.T) {
	mux, _ := serveWithPool(t)
	role := aStaffRole(t, mux, "Field Executive "+uuid.NewString()[:8], 5)
	asha := aStaffMember(t, mux, role, "Asha Nair")

	for _, body := range []string{
		`{"shifts":[{"weekday":8,"starts_at":"09:00","ends_at":"18:00"}]}`,
		`{"shifts":[{"weekday":1,"starts_at":"18:00","ends_at":"09:00"}]}`,
		`{"shifts":[{"weekday":1,"starts_at":"nine","ends_at":"18:00"}]}`,
	} {
		w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPut,
			"/v1/ops/staff/"+asha+"/shifts", body)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s was accepted as a rota: %d %s", body, w.Code, w.Body.String())
		}
	}
}

// The band the firm may set is the one the schema allows, and a role carrying
// forty buildings is the overload this whole feature exists to prevent.
func TestARoleCarryingAnImpossibleWorkloadIsRefused(t *testing.T) {
	mux, _ := serveWithPool(t)

	for _, limit := range []int{0, 51} {
		w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/roles",
			fmt.Sprintf(`{"name":"Impossible %s","permissions":["property.read"],"property_limit":%d}`,
				uuid.NewString()[:8], limit))
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("a limit of %d was accepted: %d %s", limit, w.Code, w.Body.String())
		}
	}
}

// Chapter 010's vocabulary is closed: a permission the platform does not know
// is a permission nothing will ever check.
func TestAPermissionThePlatformDoesNotKnowIsRefused(t *testing.T) {
	mux, _ := serveWithPool(t)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, "/v1/ops/staff/roles",
		fmt.Sprintf(`{"name":"Invented %s","permissions":["property.smash"],"property_limit":5}`,
			uuid.NewString()[:8]))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an invented permission was accepted: %d %s", w.Code, w.Body.String())
	}
}
