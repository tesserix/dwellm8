package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

func terms(rent int64, due domain.DueDay) domain.Terms {
	return domain.Terms{RentMinor: rent, Cycle: domain.Monthly, DueDay: due}
}

func leaseFrom(t *testing.T, fy int, fm time.Month, fd int, to ...int) domain.Lease {
	t.Helper()
	return domain.Lease{
		ID: "lease-1", TenantID: "org-1", Property: "prop-1", Unit: "unit-1",
		State: domain.StateActive, Term: interval(t, fy, fm, fd, to...),
	}
}

// The story's primary scenario: a tenancy from 5 August at ₹27,500 due on the
// 5th. The start is on the due day, so the first charge is a whole period — and
// asserting that is the point, because "the first charge is prorated" is only
// true when the start does not land on the due day.
func TestATenancyStartingOnItsDueDayChargesAWholePeriod(t *testing.T) {
	l := leaseFrom(t, 2026, 8, 5, 2027, 8, 5)

	got, err := l.Schedule(terms(27_500_00, 5), effective.Day(2026, 11, 1))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("generated %d charges to 1 November, want 3", len(got))
	}

	first := got[0]
	if first.Partial() {
		t.Errorf("the first charge is prorated at %d/%d days — a tenancy starting on its own due "+
			"day owes a whole period", first.Days, first.InPeriod)
	}
	if !first.DueOn.Equal(effective.Day(2026, 8, 5)) {
		t.Errorf("the first charge falls due %s, want 2026-08-05", first.DueOn)
	}
	if !first.To.Equal(effective.Day(2026, 9, 5)) {
		t.Errorf("the first charge covers to %s, want 2026-09-05", first.To)
	}
	for i, p := range got {
		if p.Seq != i {
			t.Errorf("charge %d is sequenced %d", i, p.Seq)
		}
		if p.Partial() {
			t.Errorf("charge %d is partial: %+v", i, p)
		}
	}
}

// The story's edge case. A tenancy starting mid-period owes the part of it that
// it occupies, charged on the day the tenant moves in — not on a 5th that has
// already gone.
func TestATenancyStartingMidPeriodProratesItsFirstCharge(t *testing.T) {
	l := leaseFrom(t, 2026, 8, 20, 2027, 8, 5)

	got, err := l.Schedule(terms(27_500_00, 5), effective.Day(2026, 10, 1))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}

	first := got[0]
	if !first.Partial() {
		t.Fatal("a tenancy starting on the 20th owes a whole period, apparently")
	}
	if !first.DueOn.Equal(effective.Day(2026, 8, 20)) {
		t.Errorf("the part charge falls due %s, want the day occupancy began", first.DueOn)
	}
	// 20 August to 5 September is 16 days of the 31-day period 5 Aug – 5 Sep.
	if first.Days != 16 || first.InPeriod != 31 {
		t.Errorf("the part charge is %d/%d days, want 16/31", first.Days, first.InPeriod)
	}
	if !first.From.Equal(effective.Day(2026, 8, 20)) || !first.To.Equal(effective.Day(2026, 9, 5)) {
		t.Errorf("the part charge covers %s to %s", first.From, first.To)
	}
	if got[1].Partial() {
		t.Errorf("the second charge is still partial: %+v", got[1])
	}
}

// The last charge is cut by the day occupancy ceases, not by the agreement —
// ADR-0010's distinction, applied to money.
func TestTheLastChargeIsCutByOccupancyRatherThanByTheAgreement(t *testing.T) {
	l := leaseFrom(t, 2026, 8, 5, 2027, 8, 5)
	l.EndedOn = effective.Day(2026, 10, 20)

	got, err := l.Schedule(terms(30_000_00, 5), effective.Day(2027, 8, 5))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("generated %d charges, want 3 — August, September and part of October", len(got))
	}

	last := got[2]
	if !last.Partial() {
		t.Fatal("the final charge covers a whole period on a tenancy that ended on the 20th")
	}
	// 5 October to 20 October is 15 days of the 31-day period 5 Oct – 5 Nov.
	if last.Days != 15 || last.InPeriod != 31 {
		t.Errorf("the final charge is %d/%d days, want 15/31", last.Days, last.InPeriod)
	}
	if !last.To.Equal(effective.Day(2026, 10, 20)) {
		t.Errorf("the final charge covers to %s, want the day occupancy ceased", last.To)
	}
}

// A due day of the 31st means the last day of the month, whatever it is. The
// alternative reading — skip the months that have no 31st — bills nine times a
// year.
func TestADueDayOfTheThirtyFirstIsTheLastDayOfTheMonth(t *testing.T) {
	l := leaseFrom(t, 2027, 12, 31, 2028, 6, 30)

	got, err := l.Schedule(terms(20_000_00, 31), effective.Day(2028, 6, 1))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}

	want := []effective.Date{
		effective.Day(2027, 12, 31),
		effective.Day(2028, 1, 31),
		effective.Day(2028, 2, 29), // 2028 is a leap year
		effective.Day(2028, 3, 31),
		effective.Day(2028, 4, 30),
		effective.Day(2028, 5, 31),
	}
	if len(got) != len(want) {
		t.Fatalf("generated %d charges, want %d — a due day of the 31st must not skip February",
			len(got), len(want))
	}
	for i, w := range want {
		if !got[i].DueOn.Equal(w) {
			t.Errorf("charge %d falls due %s, want %s", i, got[i].DueOn, w)
		}
		if got[i].Partial() {
			t.Errorf("charge %d on a due-day-31 schedule is partial: %+v — the month being short "+
				"moves the day, it does not shorten the period", i, got[i])
		}
	}
}

// A periodic tenancy has no end, so the horizon is the only thing bounding it.
func TestAnOpenEndedTenancyIsBoundedByTheHorizon(t *testing.T) {
	l := leaseFrom(t, 2026, 4, 1)

	got, err := l.Schedule(terms(15_000_00, 1), effective.Day(2026, 9, 1))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("generated %d charges to 1 September, want 6", len(got))
	}
	if !got[len(got)-1].DueOn.Equal(effective.Day(2026, 9, 1)) {
		t.Errorf("the last charge falls due %s", got[len(got)-1].DueOn)
	}
}

// A quarterly cycle charges a quarter's rent every three months, and the amount
// is the cycle's rather than a month's.
func TestAQuarterlyCycleChargesEveryThreeMonths(t *testing.T) {
	l := leaseFrom(t, 2026, 4, 1, 2027, 4, 1)
	q := domain.Terms{RentMinor: 90_000_00, Cycle: domain.Quarterly, DueDay: 1}

	got, err := l.Schedule(q, effective.Day(2027, 4, 1))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("generated %d charges in a year, want 4", len(got))
	}
	for i, p := range got {
		if p.Partial() {
			t.Errorf("quarter %d is partial: %+v", i, p)
		}
	}
	if !got[1].DueOn.Equal(effective.Day(2026, 7, 1)) {
		t.Errorf("the second quarter falls due %s, want 2026-07-01", got[1].DueOn)
	}
}

// A regenerated schedule reproduces what was raised, which is the whole reason
// the horizon is an argument.
func TestASchedulePregeneratedInNovemberStillAgreesAboutAugust(t *testing.T) {
	l := leaseFrom(t, 2026, 8, 20, 2027, 8, 5)
	tm := terms(27_500_00, 5)

	inAugust, err := l.Schedule(tm, effective.Day(2026, 8, 31))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	inNovember, err := l.Schedule(tm, effective.Day(2026, 11, 30))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if len(inNovember) <= len(inAugust) {
		t.Fatal("November generated no more charges than August")
	}
	for i := range inAugust {
		if inAugust[i] != inNovember[i] {
			t.Errorf("charge %d differs between runs:\n  August:   %+v\n  November: %+v",
				i, inAugust[i], inNovember[i])
		}
	}
}

// Terms that cannot bill are refused where they are entered.
func TestTermsThatCannotBillAreRefused(t *testing.T) {
	l := leaseFrom(t, 2026, 8, 5, 2027, 8, 5)

	for _, c := range []struct {
		name string
		t    domain.Terms
	}{
		{"no rent", domain.Terms{Cycle: domain.Monthly, DueDay: 5}},
		{"no cycle", domain.Terms{RentMinor: 100, DueDay: 5}},
		{"a due day of the 0th", domain.Terms{RentMinor: 100, Cycle: domain.Monthly}},
		{"a due day of the 32nd", domain.Terms{RentMinor: 100, Cycle: domain.Monthly, DueDay: 32}},
		{"a deposit nobody holds", domain.Terms{RentMinor: 100, Cycle: domain.Monthly, DueDay: 5,
			DepositMinor: 5000}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := l.Schedule(c.t, effective.Day(2026, 12, 1)); !errors.Is(err, domain.ErrTerms) {
				t.Errorf("scheduled anyway: %v", err)
			}
		})
	}
}

// The deposit is not a first month at double rent. It is its own charge, posted
// to a liability rather than to income.
func TestTheDepositIsItsOwnChargeAndNotRent(t *testing.T) {
	l := leaseFrom(t, 2026, 8, 5, 2027, 8, 5)
	tm := terms(27_500_00, 5)
	tm.DepositMinor, tm.DepositHeldBy = 82_500_00, domain.HeldByOwner

	periods, err := l.Schedule(tm, effective.Day(2026, 10, 1))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	for _, p := range periods {
		if p.Days == 0 {
			t.Errorf("a charge covering no days: %+v", p)
		}
	}

	d, ok := l.Deposit(tm)
	if !ok {
		t.Fatal("a deposit of ₹82,500 produced no charge")
	}
	if d.AmountMinor != 82_500_00 || !d.DueOn.Equal(effective.Day(2026, 8, 5)) {
		t.Errorf("the deposit charge is %+v", d)
	}
	if d.HeldBy != domain.HeldByOwner {
		t.Errorf("the deposit is held by %q", d.HeldBy)
	}

	if _, ok := l.Deposit(terms(27_500_00, 5)); ok {
		t.Error("a tenancy with no deposit produced a deposit charge")
	}
}

// Creating a lease: it starts in draft, publishes lease.created, and a draft
// needs somebody liable for the rent.
func TestCreatingALeaseProducesADraftAndOneEvent(t *testing.T) {
	d := domain.Draft{
		TenantID: "org-1", Property: "prop-1", Unit: "unit-1",
		Term: interval(t, 2026, 8, 5, 2027, 8, 5), NoticeDays: 60,
		Terms: terms(27_500_00, 5),
		Parties: []domain.Party{
			{PartyID: "p1", Role: domain.RoleTenant, Name: "Ravi Menon", Phone: "+919876543210"},
			{PartyID: "p2", Role: domain.RoleOccupant, Name: "Anita Menon"},
		},
	}

	l, tm, ev, err := d.Create()
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if l.State != domain.StateDraft {
		t.Errorf("a new lease is in state %s, want draft", l.State)
	}
	if ev != domain.EventCreated {
		t.Errorf("creation published %q", ev)
	}
	if tm.RentMinor != 27_500_00 {
		t.Errorf("the terms came back as %+v", tm)
	}

	// A draft is not one signature away from being a tenancy: it goes out for
	// signature first, which is the state machine's business rather than this
	// story's.
	if _, _, err := l.Activate(domain.ActorOwner); !errors.Is(err, domain.ErrTransition) {
		t.Errorf("a draft activated directly: %v", err)
	}

	// And once it is countersigned it still cannot start, because nothing says
	// which TDS section governs it. ADR-0024.
	l.State = domain.StatePendingSignature
	if _, _, err := l.Activate(domain.ActorOwner); !errors.Is(err, domain.ErrTaxFacts) {
		t.Errorf("a tenancy with no tax facts started: %v", err)
	}
}

func TestADraftWithoutATenantIsRefused(t *testing.T) {
	base := domain.Draft{
		TenantID: "org-1", Property: "prop-1", Unit: "unit-1",
		Term: interval(t, 2026, 8, 5, 2027, 8, 5), Terms: terms(27_500_00, 5),
	}

	for _, c := range []struct {
		name    string
		parties []domain.Party
	}{
		{"nobody at all", nil},
		{"an occupant only", []domain.Party{
			{PartyID: "p1", Role: domain.RoleOccupant, Name: "Anita Menon"}}},
		{"a tenant with no reachable number", []domain.Party{
			{PartyID: "p1", Role: domain.RoleTenant, Name: "Ravi Menon", Phone: "9876543210"}}},
		{"a tenant with no name", []domain.Party{
			{PartyID: "p1", Role: domain.RoleTenant, Phone: "+919876543210"}}},
		{"the same person twice", []domain.Party{
			{PartyID: "p1", Role: domain.RoleTenant, Name: "Ravi Menon", Phone: "+919876543210"},
			{PartyID: "p1", Role: domain.RoleGuarantor, Name: "Ravi Menon", Phone: "+919876543210"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := base
			d.Parties = c.parties
			if _, _, _, err := d.Create(); !errors.Is(err, domain.ErrDraft) {
				t.Errorf("created anyway: %v", err)
			}
		})
	}
}

// The story's second edge case, end to end: a non-resident landlord and a lease
// that will not start until the tenant has accepted the obligation.
func TestANonResidentLandlordBlocksActivationUntilAcknowledged(t *testing.T) {
	facts := tds.Facts{
		Deductor: tds.Business, Residency: tds.NonResident,
		From: effective.Day(2026, 8, 5), Source: "landlord declaration",
	}
	d := domain.Draft{
		TenantID: "org-1", Property: "prop-1", Unit: "unit-1",
		Term: interval(t, 2026, 8, 5, 2027, 8, 5), Terms: terms(27_500_00, 5),
		Parties: []domain.Party{
			{PartyID: "p1", Role: domain.RoleTenant, Name: "Ravi Menon", Phone: "+919876543210"}},
		Tax: history(t, effective.Record[tds.Facts]{
			ID: "1", Range: interval(t, 2026, 8, 5), Kind: effective.KindChange, Value: facts}),
	}

	l, _, _, err := d.Create()
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	l.State = domain.StatePendingSignature
	if _, _, err := l.Activate(domain.ActorOwner); !errors.Is(err, tds.ErrNotAcknowledged) {
		t.Fatalf("a section 195 tenancy started unacknowledged: %v", err)
	}

	facts.AcknowledgedOn, facts.AcknowledgedBy = effective.Day(2026, 8, 1), "tenant:ravi"
	l.Tax = history(t, effective.Record[tds.Facts]{
		ID: "1", Range: interval(t, 2026, 8, 5), Kind: effective.KindChange, Value: facts})
	if _, _, err := l.Activate(domain.ActorOwner); err != nil {
		t.Errorf("an acknowledged tenancy was still refused: %v", err)
	}
}
