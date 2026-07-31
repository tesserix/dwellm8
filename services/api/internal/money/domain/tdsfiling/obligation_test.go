package tdsfiling_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/tdsfiling"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

func period(t *testing.T, fy int, fm time.Month, fd, ty int, tm time.Month, td int) effective.Interval {
	t.Helper()
	iv, err := effective.Between(effective.Day(fy, fm, fd), effective.Day(ty, tm, td))
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	return iv
}

func due(t *testing.T, o tdsfiling.Obligation, step tdsfiling.Step) tdsfiling.Due {
	t.Helper()
	for _, d := range o.Schedule {
		if d.Step == step {
			return d
		}
	}
	t.Fatalf("section %s has no %s step: %+v", o.Section, step, o.Schedule)
	return tdsfiling.Due{}
}

// #87's primary scenario: a company tenant paying monthly to a resident
// landlord. Deposit by the 7th of the next month, report quarterly on 26Q,
// certify on Form 16A.
func TestASection194IDeductionDepositsBySeventhAndReportsQuarterly(t *testing.T) {
	o, err := tdsfiling.New(tds.Section194I, "lease-1",
		period(t, 2026, 8, 5, 2026, 9, 5), effective.Day(2026, 8, 5),
		9_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}

	if got := due(t, o, tdsfiling.Deposit); !got.By.Equal(effective.Day(2026, 9, 7)) {
		t.Errorf("deposit is due %s, want 2026-09-07", got.By)
	}
	report := due(t, o, tdsfiling.Report)
	if !report.By.Equal(effective.Day(2026, 10, 31)) {
		t.Errorf("the return is due %s, want 2026-10-31 — August is in the July–September quarter",
			report.By)
	}
	if report.Artefact != tds.Return26Q {
		t.Errorf("a resident landlord is reported on %s, want 26Q", report.Artefact)
	}
	if got := due(t, o, tdsfiling.Certify); got.Artefact != tds.Form16A ||
		!got.By.Equal(effective.Day(2026, 11, 15)) {
		t.Errorf("the certificate is %s due %s, want Form 16A on 2026-11-15", got.Artefact, got.By)
	}
}

// A payment to a non-resident is reported on 27Q, not 26Q. Filing the wrong one
// is a defective return rather than a typo.
func TestASection195DeductionIsReportedOn27Q(t *testing.T) {
	o, err := tdsfiling.New(tds.Section195, "lease-1",
		period(t, 2026, 8, 5, 2026, 9, 5), effective.Day(2026, 8, 5),
		6_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if got := due(t, o, tdsfiling.Report); got.Artefact != tds.Return27Q {
		t.Errorf("a non-resident's deduction is reported on %s, want 27Q", got.Artefact)
	}
}

// The two dates everybody gets wrong: March deposits by 30 April rather than
// 7 April, and the January–March return is due 31 May rather than 30 April.
func TestMarchIsTheExceptionOnBothClocks(t *testing.T) {
	o, err := tdsfiling.New(tds.Section194I, "lease-1",
		period(t, 2027, 3, 5, 2027, 4, 5), effective.Day(2027, 3, 5),
		9_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if got := due(t, o, tdsfiling.Deposit); !got.By.Equal(effective.Day(2027, 4, 30)) {
		t.Errorf("March's deposit is due %s, want 2027-04-30", got.By)
	}
	if got := due(t, o, tdsfiling.Report); !got.By.Equal(effective.Day(2027, 5, 31)) {
		t.Errorf("the January–March return is due %s, want 2027-05-31", got.By)
	}
}

// A government deductor adjusts by book entry and never holds a challan.
func TestAGovernmentDeductorDepositsByBookEntry(t *testing.T) {
	o, err := tdsfiling.New(tds.Section194I, "lease-1",
		period(t, 2026, 8, 5, 2026, 9, 5), effective.Day(2026, 8, 5),
		9_000_00, true, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if got := due(t, o, tdsfiling.Deposit); got.Artefact != tds.BookEntry24G {
		t.Errorf("a government deductor deposits with %s", got.Artefact)
	}
}

// #86's primary scenario. Section 194-IB deducts once, and its whole filing is
// one form — so there is no separate deposit step to remind anybody about.
func TestASection194IBDeductionIsOnceAYearAndOneForm(t *testing.T) {
	o, err := tdsfiling.New(tds.Section194IB, "lease-1",
		period(t, 2026, 4, 1, 2027, 4, 1), effective.Day(2026, 6, 5),
		12_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}

	if got := due(t, o, tdsfiling.Deduct); !got.By.Equal(effective.Day(2027, 3, 31)) {
		t.Errorf("the deduction falls on %s, want the financial year end 2027-03-31", got.By)
	}
	report := due(t, o, tdsfiling.Report)
	if report.Artefact != tds.Form26QC {
		t.Errorf("194-IB reports on %s, want 26QC", report.Artefact)
	}
	// 30 days from the end of March.
	if !report.By.Equal(effective.Day(2027, 4, 30)) {
		t.Errorf("Form 26QC is due %s, want 2027-04-30", report.By)
	}
	if got := due(t, o, tdsfiling.Certify); got.Artefact != tds.Form16C ||
		!got.By.Equal(effective.Day(2027, 5, 15)) {
		t.Errorf("the certificate is %s due %s, want Form 16C on 2027-05-15", got.Artefact, got.By)
	}

	for _, d := range o.Schedule {
		if d.Step == tdsfiling.Deposit {
			t.Error("194-IB has a separate deposit step — 26QC is a challan-cum-statement, so a " +
				"reminder for a challan is a reminder for a document that does not exist")
		}
	}
}

// The half of 194-IB that products forget: a tenant who leaves in November
// deducts in November, not the following March.
func TestASection194IBTenancyEndingMidYearDeductsAtItsOwnEnd(t *testing.T) {
	o, err := tdsfiling.New(tds.Section194IB, "lease-1",
		period(t, 2026, 4, 1, 2026, 11, 20), effective.Day(2026, 6, 5),
		8_000_00, false, effective.Day(2026, 11, 20))
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if got := due(t, o, tdsfiling.Deduct); !got.By.Equal(effective.Day(2026, 11, 20)) {
		t.Errorf("the deduction falls on %s, want the day the tenancy ended", got.By)
	}
	// 30 days from the end of November.
	if got := due(t, o, tdsfiling.Report); !got.By.Equal(effective.Day(2026, 12, 30)) {
		t.Errorf("Form 26QC is due %s, want 2026-12-30", got.By)
	}
}

// Both stories' failure scenario: the deadline passes, the item stays open, and
// the warning states what it costs rather than colouring it red.
func TestAMissedDeadlineStatesWhatItCosts(t *testing.T) {
	o, err := tdsfiling.New(tds.Section194I, "lease-1",
		period(t, 2026, 8, 5, 2026, 9, 5), effective.Day(2026, 8, 5),
		9_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}

	// Deducted on time, not deposited. Two months later.
	o, err = o.Record(tdsfiling.Deduct, "internal-1", effective.Day(2026, 8, 5))
	if err != nil {
		t.Fatalf("recording the deduction: %v", err)
	}
	on := effective.Day(2026, 11, 10)

	overdue := o.Overdue(on)
	if len(overdue) == 0 {
		t.Fatal("a deposit two months late is not overdue")
	}
	if overdue[0].Step != tdsfiling.Deposit {
		t.Errorf("the first overdue step is %s, want the deposit", overdue[0].Step)
	}

	cons := o.Consequences(on)
	if len(cons) == 0 {
		t.Fatal("an overdue deposit has no stated consequence")
	}
	first := cons[0]
	if first.Under != "201(1A)(ii)" {
		t.Errorf("the deposit default is charged under %q", first.Under)
	}
	if !strings.Contains(first.Because, "1.5%") {
		t.Errorf("the warning does not state the rate: %q", first.Because)
	}
	if first.MonthsRunning < 2 {
		t.Errorf("interest has run %d months since 7 September, want at least 2", first.MonthsRunning)
	}

	// And it stays open: recording the deposit is what closes it.
	if _, open := o.Next(); !open {
		t.Error("the obligation reports nothing outstanding while the deposit is unpaid")
	}
	o, err = o.Record(tdsfiling.Deposit, "CIN-0001234", effective.Day(2026, 11, 10))
	if err != nil {
		t.Fatalf("recording the challan: %v", err)
	}
	for _, d := range o.Overdue(on) {
		if d.Step == tdsfiling.Deposit {
			t.Error("the deposit is still overdue after its challan was recorded")
		}
	}
}

// Failing to deduct at all is the cheaper rate and the one where the tax is
// still owed — and the warning has to say so, because a tenant who never
// deducted believes they have nothing to pay.
func TestFailingToDeductSaysTheTaxIsStillOwed(t *testing.T) {
	o, err := tdsfiling.New(tds.Section195, "lease-1",
		period(t, 2026, 8, 5, 2026, 9, 5), effective.Day(2026, 8, 5),
		6_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}

	cons := o.Consequences(effective.Day(2026, 10, 1))
	if len(cons) == 0 || cons[0].Step != tdsfiling.Deduct {
		t.Fatalf("a deduction never made produced %+v", cons)
	}
	for _, want := range []string{"1%", "still owed", "deductor"} {
		if !strings.Contains(cons[0].Because, want) {
			t.Errorf("the warning omits %q: %q", want, cons[0].Because)
		}
	}
}

// A step cannot be closed without the reference that proves it.
func TestAStepCannotBeRecordedWithoutItsReference(t *testing.T) {
	o, err := tdsfiling.New(tds.Section194I, "lease-1",
		period(t, 2026, 8, 5, 2026, 9, 5), effective.Day(2026, 8, 5),
		9_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}

	if _, err := o.Record(tdsfiling.Deposit, "", effective.Day(2026, 9, 5)); !errors.Is(err, tdsfiling.ErrObligation) {
		t.Errorf("a deposit was recorded with no challan number: %v", err)
	}
	if _, err := o.Record(tdsfiling.Deposit, "CIN-1", effective.Date{}); !errors.Is(err, tdsfiling.ErrObligation) {
		t.Errorf("a deposit was recorded with no date: %v", err)
	}
	// 194-IB has no deposit step, so recording one is a caller confusing its
	// sections rather than a typo.
	ib, err := tdsfiling.New(tds.Section194IB, "lease-1",
		period(t, 2026, 4, 1, 2027, 4, 1), effective.Day(2026, 6, 5),
		12_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	if _, err := ib.Record(tdsfiling.Deposit, "CIN-1", effective.Day(2027, 4, 1)); !errors.Is(err, tdsfiling.ErrObligation) {
		t.Errorf("a challan was recorded against a 194-IB obligation: %v", err)
	}
}

// An obligation that cannot be scheduled is refused where it is built.
func TestAnObligationThatCannotBeScheduledIsRefused(t *testing.T) {
	p := period(t, 2026, 8, 5, 2026, 9, 5)
	for _, c := range []struct {
		name    string
		section tds.Section
		lease   string
		paid    effective.Date
		amount  int64
	}{
		{"no lease", tds.Section194I, "", effective.Day(2026, 8, 5), 100},
		{"no payment date", tds.Section194I, "lease-1", effective.Date{}, 100},
		{"nothing deducted", tds.Section194I, "lease-1", effective.Day(2026, 8, 5), 0},
		{"a section this package does not schedule", tds.Section("194ia"), "lease-1",
			effective.Day(2026, 8, 5), 100},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := tdsfiling.New(c.section, c.lease, p, c.paid, c.amount, false, effective.Date{})
			if !errors.Is(err, tdsfiling.ErrObligation) {
				t.Errorf("scheduled anyway: %v", err)
			}
		})
	}
}
