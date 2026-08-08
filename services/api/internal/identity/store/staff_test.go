package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The managers a firm employs (#353). What is worth testing here is the cap: a
// firm that hands one person twenty buildings has twenty neglected buildings,
// and a limit a handler can forget to check is not a limit.

func aProperty(t *testing.T, plat tenancy.PlatformPool, org tenancy.ID, code string) string {
	t.Helper()
	id := uuid.NewString()
	err := tenancy.Platform(context.Background(), plat, "a property to be responsible for",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO properties (id, tenant_id, code, name, kind,
				                        address_line1, locality, city, state_code, pin)
				VALUES ($1::uuid, $2::uuid, $3::citext, $3::text, 'building',
				        '1 Road', 'Kaloor', 'Kochi', 'KL', '682001')`, id, string(org), code)
			return err
		})
	if err != nil {
		t.Fatalf("creating a property: %v", err)
	}
	return id
}

func aRole(t *testing.T, s *store.Principals, org tenancy.ID, name string, limit int) string {
	t.Helper()
	id, err := s.SaveStaffRole(context.Background(), org, store.StaffRole{
		Name: name, Permissions: []string{"property.read", "maintenance.write"}, PropertyLimit: limit,
	})
	if err != nil {
		t.Fatalf("creating a role: %v", err)
	}
	return id
}

func aManager(t *testing.T, s *store.Principals, org tenancy.ID, role, name string) string {
	t.Helper()
	m, err := s.AddStaffMember(context.Background(), org, store.StaffMember{
		PartyID: uuid.NewString(), RoleID: role, FullName: name,
		Phone: "+919876500001", Email: "asha@example.test",
		Designation: "Field executive", EmploymentType: "full_time",
		JoinedOn: "2026-01-05", PANMasked: "XXXXXX234F",
		SalaryMinor: 4500000, PayFrequency: "monthly", State: "active",
	})
	if err != nil {
		t.Fatalf("onboarding a manager: %v", err)
	}
	return m.ID
}

func TestAManagerIsEmployedWithTheirTermsAndReadBack(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "staff-employ")
	role := aRole(t, s, firm, "Field Executive", 5)
	id := aManager(t, s, firm, role, "Asha Nair")

	held, err := s.StaffMembers(ctx, firm)
	if err != nil {
		t.Fatalf("reading the team: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("the firm has %d people, wanted the one it employed", len(held))
	}
	got := held[0]
	if got.ID != id || got.FullName != "Asha Nair" || got.RoleName != "Field Executive" {
		t.Errorf("the manager came back as %+v", got)
	}
	if got.SalaryMinor != 4500000 || got.PANMasked != "XXXXXX234F" || got.JoinedOn != "2026-01-05" {
		t.Errorf("the terms came back as %+v", got)
	}
	// The role's limit is what applies until this person is given their own.
	if got.PropertyLimit != 5 || got.Held != 0 {
		t.Errorf("the load came back as %d of %d", got.Held, got.PropertyLimit)
	}
}

// ADR-0013: the record holds a mask, never the number.
func TestAWholePANIsRefusedOnTheEmploymentRecord(t *testing.T) {
	s, _ := principals(t)
	firm := aFirm(t, s, "staff-pan")
	role := aRole(t, s, firm, "Field Executive", 5)

	_, err := s.AddStaffMember(context.Background(), firm, store.StaffMember{
		PartyID: uuid.NewString(), RoleID: role, FullName: "Asha Nair",
		Phone: "+919876500001", PANMasked: "ABCDE1234F", State: "active",
	})
	if err == nil {
		t.Error("a whole PAN was written to the employment record")
	}
}

func TestOneManagerIsNotGivenMorePropertiesThanTheRoleAllows(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "staff-cap")
	role := aRole(t, s, firm, "Field Executive", 2)
	asha := aManager(t, s, firm, role, "Asha Nair")

	for i, code := range []string{"BLK1", "BLK2"} {
		if _, err := s.AssignProperty(ctx, firm, asha, aProperty(t, plat, firm, code)); err != nil {
			t.Fatalf("assignment %d: %v", i+1, err)
		}
	}

	_, err := s.AssignProperty(ctx, firm, asha, aProperty(t, plat, firm, "BLK3"))
	if !errors.Is(err, store.ErrOverCap) {
		t.Fatalf("the third property came back as %v, wanted the cap", err)
	}

	// The firm can still say this one person carries more than their role does.
	if err := s.SetStaffLimit(ctx, firm, asha, 3); err != nil {
		t.Fatalf("raising the limit: %v", err)
	}
	if _, err := s.AssignProperty(ctx, firm, asha, aProperty(t, plat, firm, "BLK4")); err != nil {
		t.Fatalf("the third property under a raised limit: %v", err)
	}
}

// A building two people are responsible for is a building nobody is.
func TestAPropertyAlreadyHeldIsNotGivenToASecondManager(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "staff-taken")
	role := aRole(t, s, firm, "Field Executive", 5)
	asha := aManager(t, s, firm, role, "Asha Nair")
	ravi := aManager(t, s, firm, role, "Ravi Menon")
	block := aProperty(t, plat, firm, "BLK1")

	if _, err := s.AssignProperty(ctx, firm, asha, block); err != nil {
		t.Fatalf("the first assignment: %v", err)
	}
	if _, err := s.AssignProperty(ctx, firm, ravi, block); !errors.Is(err, store.ErrPropertyHeld) {
		t.Fatalf("the second manager came back as %v, wanted the building already held", err)
	}
}

func TestHandingAPropertyBackFreesTheSlotSameDay(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "staff-release")
	role := aRole(t, s, firm, "Field Executive", 1)
	asha := aManager(t, s, firm, role, "Asha Nair")
	ravi := aManager(t, s, firm, role, "Ravi Menon")
	block := aProperty(t, plat, firm, "BLK1")

	id, err := s.AssignProperty(ctx, firm, asha, block)
	if err != nil {
		t.Fatalf("the first assignment: %v", err)
	}
	if err := s.ReleaseAssignment(ctx, firm, id); err != nil {
		t.Fatalf("handing it back: %v", err)
	}
	if _, err := s.AssignProperty(ctx, firm, ravi, block); err != nil {
		t.Fatalf("giving it to somebody else the same day: %v", err)
	}

	team, err := s.StaffMembers(ctx, firm)
	if err != nil {
		t.Fatalf("reading the team: %v", err)
	}
	for _, m := range team {
		want := 0
		if m.ID == ravi {
			want = 1
		}
		if m.Held != want {
			t.Errorf("%s holds %d properties, wanted %d", m.FullName, m.Held, want)
		}
	}
}

// The rota is edited as a week, not row by row: what the firm sends is what the
// week becomes, so a shift dropped from the form is a shift dropped.
func TestTheRotaIsReplacedAsAWholeWeek(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "staff-rota")
	role := aRole(t, s, firm, "Field Executive", 5)
	asha := aManager(t, s, firm, role, "Asha Nair")

	week := []store.StaffShift{
		{Weekday: 1, StartsAt: "09:00", EndsAt: "18:00"},
		{Weekday: 2, StartsAt: "09:00", EndsAt: "18:00"},
		{Weekday: 6, StartsAt: "10:00", EndsAt: "14:00"},
	}
	if err := s.SetStaffShifts(ctx, firm, asha, week); err != nil {
		t.Fatalf("setting the rota: %v", err)
	}
	if err := s.SetStaffShifts(ctx, firm, asha, week[:2]); err != nil {
		t.Fatalf("shortening the rota: %v", err)
	}

	got, err := s.StaffShifts(ctx, firm, asha)
	if err != nil {
		t.Fatalf("reading the rota: %v", err)
	}
	if len(got) != 2 || got[0].Weekday != 1 || got[1].EndsAt != "18:00" {
		t.Fatalf("the week came back as %+v", got)
	}
}

func TestAShiftThatEndsBeforeItStartsIsRefused(t *testing.T) {
	s, _ := principals(t)
	firm := aFirm(t, s, "staff-rota-bad")
	role := aRole(t, s, firm, "Field Executive", 5)
	asha := aManager(t, s, firm, role, "Asha Nair")

	err := s.SetStaffShifts(context.Background(), firm, asha,
		[]store.StaffShift{{Weekday: 2, StartsAt: "18:00", EndsAt: "09:00"}})
	if err == nil {
		t.Error("a shift ending before it starts was written to the rota")
	}
}
