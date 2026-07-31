package tds_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

// registry mirrors the rows the schema seeds for TDS, including the pair either
// side of 1 April 2025 — the threshold change is what makes the as-of argument do
// visible work — and deliberately omits any section 195 rate, because there is no
// single number to hold and the product must say so rather than guess.
func registry(t *testing.T) *tds.Matrix {
	t.Helper()

	rule := func(typ statutory.Type, key string, iv effective.Interval) statutory.Rule {
		return statutory.Rule{
			ID: key + "@" + iv.String(), Type: typ, Jurisdiction: statutory.National, Key: key,
			Validity: iv, StatuteRef: "Income-tax Act 1961", Verification: statutory.NeedsBareActCheck,
			Owner: "compliance", ReviewDue: effective.Day(2027, 1, 1), Enforcement: statutory.Warn,
		}
	}
	rate := func(key string, bps int, iv effective.Interval) statutory.Rule {
		r := rule(statutory.TDSRate, key, iv)
		r.Kind, r.RateBps = statutory.KindRate, bps
		return r
	}
	amount := func(key string, minor int64, iv effective.Interval) statutory.Rule {
		r := rule(statutory.TDSThreshold, key, iv)
		r.Kind, r.AmountMinor = statutory.KindAmount, minor
		return r
	}

	table, err := statutory.NewTable([]statutory.Rule{
		rate("tds.194i_land_and_building", 1000, since(t, 2020, 4, 1)),
		amount("tds.194i_annual", 24_000_00, between(t, 2020, 4, 1, 2025, 4, 1)),
		amount("tds.194i_annual", 6_00_000_00, since(t, 2025, 4, 1)),
		rate("tds.194ib_individual_huf", 500, between(t, 2020, 4, 1, 2024, 10, 1)),
		rate("tds.194ib_individual_huf", 200, since(t, 2024, 10, 1)),
		amount("tds.194ib_monthly", 50_000_00, since(t, 2020, 4, 1)),
	})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}
	m, err := tds.New(table)
	if err != nil {
		t.Fatalf("binding the matrix: %v", err)
	}
	return m
}

func since(t *testing.T, y int, m time.Month, d int) effective.Interval {
	t.Helper()
	iv, err := effective.Since(effective.Day(y, m, d))
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	return iv
}

func between(t *testing.T, fy int, fm time.Month, fd, ty int, tm time.Month, td int) effective.Interval {
	t.Helper()
	iv, err := effective.Between(effective.Day(fy, fm, fd), effective.Day(ty, tm, td))
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	return iv
}

func facts(d tds.DeductorClass, r tds.Residency, y int, m time.Month, day int) tds.Facts {
	return tds.Facts{Deductor: d, Residency: r, From: effective.Day(y, m, day), Source: "tenant declaration"}
}

// The story's primary scenario: a company tenant paying eighty thousand a month to
// a resident landlord is on section 194-I, at the land-and-building rate, over the
// annual threshold, deducting on every payment and certifying on Form 16A.
func TestACompanyTenantAndAResidentLandlordAreSection194I(t *testing.T) {
	m := registry(t)

	a, err := m.Assess(
		facts(tds.Business, tds.Resident, 2026, 4, 1),
		tds.Rent{MonthlyMinor: 80_000_00, AnnualMinor: 9_60_000_00},
		effective.Day(2026, 5, 7),
	)
	if err != nil {
		t.Fatalf("assessing: %v", err)
	}

	if a.Path.Section != tds.Section194I {
		t.Errorf("selected section %s, want 194-I", a.Path.Section)
	}
	if a.RateBps != 1000 {
		t.Errorf("rate is %d bps, want 1000", a.RateBps)
	}
	if a.ThresholdMinor != 6_00_000_00 {
		t.Errorf("threshold is %d, want the six lakh in force from 1 April 2025", a.ThresholdMinor)
	}
	if !a.Deductible() {
		t.Errorf("₹9,60,000 a year does not exceed ₹6,00,000, apparently: %s", a.Because)
	}
	if a.Path.Basis != tds.BasisAnnual || a.Path.Timing != tds.EachPayment {
		t.Errorf("periodicity is %s/%s, want annual/each_payment", a.Path.Basis, a.Path.Timing)
	}
	if !a.Path.RequiresTAN {
		t.Error("a section 194-I deductor needs a TAN")
	}
	if !slices.Contains(a.Path.Artefacts, tds.Form16A) {
		t.Errorf("artefacts are %v, and 194-I certifies on Form 16A", a.Path.Artefacts)
	}
	if a.RateRuleID == "" || a.ThresholdRuleID == "" {
		t.Error("the assessment does not name the rows it used, so nothing computed from it can be " +
			"recomputed against them")
	}
}

// The story's failure scenario. An individual tenant would otherwise be a
// section 194-IB deductor at two per cent above fifty thousand a month; a
// non-resident landlord overrides all of that, and the lease may not be completed
// until the tenant has accepted an obligation that is entirely theirs.
func TestANonResidentLandlordPutsEveryDeductorOnSection195(t *testing.T) {
	m := registry(t)

	for _, d := range tds.DeductorClasses() {
		f := facts(d, tds.NonResident, 2026, 4, 1)
		p, err := tds.Select(f)
		if err != nil {
			t.Fatalf("selecting for %s: %v", d, err)
		}
		if p.Section != tds.Section195 {
			t.Errorf("a %s deductor paying a non-resident selected %s, want 195", d, p.Section)
		}
		if p.Basis != tds.BasisNone {
			t.Errorf("section 195 has basis %s, want none — tax runs from the first rupee", p.Basis)
		}
		if !p.RequiresAcknowledgement {
			t.Errorf("a %s deductor is not asked to acknowledge the section 195 obligation", d)
		}
		if err := p.RequireAcknowledgement(f); !errors.Is(err, tds.ErrNotAcknowledged) {
			t.Errorf("an unacknowledged lease completed anyway: %v", err)
		}
		for _, want := range []tds.Artefact{tds.Form15CA, tds.Form15CB, tds.Return27Q} {
			if !slices.Contains(p.Artefacts, want) {
				t.Errorf("a %s deductor's artefacts %v omit %s", d, p.Artefacts, want)
			}
		}
		if slices.Contains(p.Artefacts, tds.Return26Q) {
			t.Errorf("a payment to a non-resident is reported on 27Q, and %v says 26Q", p.Artefacts)
		}
	}

	// One rupee of rent, and it is still deductible: there is no small-value
	// exemption to fall below.
	f := facts(tds.IndividualNoAudit, tds.NonResident, 2026, 4, 1)
	if _, err := m.Assess(f, tds.Rent{MonthlyMinor: 100, AnnualMinor: 1200}, effective.Day(2026, 5, 1)); err == nil {
		t.Fatal("assessed a section 195 deduction at some rate — there is no verified rate in the " +
			"registry, so this had to fail rather than pick one")
	} else if !errors.Is(err, statutory.ErrNoRule) {
		t.Errorf("section 195 with no rate row failed with %v, want a named gap", err)
	}
}

// Acknowledgement completes the lease, and only for the facts it was given
// against.
func TestAnAcknowledgementCoversTheFactsItWasGivenAgainst(t *testing.T) {
	f := facts(tds.Business, tds.NonResident, 2026, 10, 1)
	p, err := tds.Select(f)
	if err != nil {
		t.Fatalf("selecting: %v", err)
	}

	f.AcknowledgedOn, f.AcknowledgedBy = effective.Day(2026, 10, 3), "tenant:ravi"
	if err := p.RequireAcknowledgement(f); err != nil {
		t.Errorf("an acknowledged section 195 lease was still refused: %v", err)
	}

	// Signing precedes the tenancy, so the first set of facts may be acknowledged
	// before it takes effect.
	first := facts(tds.Business, tds.NonResident, 2026, 10, 1)
	first.AcknowledgedOn, first.AcknowledgedBy = effective.Day(2026, 9, 20), "tenant:ravi"
	if _, err := tds.NewHistory([]effective.Record[tds.Facts]{
		{ID: "1", Range: since(t, 2026, 10, 1), Value: first, Kind: effective.KindChange},
	}); err != nil {
		t.Errorf("a lease acknowledged at signing was refused: %v", err)
	}

	// The tenant accepted a section 195 obligation in April, before the landlord
	// left the country. Copying that date onto October's facts does not acknowledge
	// the obligation that arose in October.
	april := facts(tds.Business, tds.Resident, 2026, 4, 1)
	october := facts(tds.Business, tds.NonResident, 2026, 10, 1)
	october.AcknowledgedOn, october.AcknowledgedBy = effective.Day(2026, 4, 5), "tenant:ravi"
	if _, err := tds.NewHistory([]effective.Record[tds.Facts]{
		{ID: "1", Range: between(t, 2026, 4, 1, 2026, 10, 1), Value: april, Kind: effective.KindChange},
		{ID: "2", Range: since(t, 2026, 10, 1), Value: october, Kind: effective.KindChange},
	}); !errors.Is(err, tds.ErrFacts) {
		t.Errorf("an acknowledgement carried forward onto the facts that replaced it was accepted: %v", err)
	}
}

// The threshold is what the proviso says: exceeded, not reached. Rent exactly at
// the limit deducts nothing, and the off-by-one in the other direction deducts tax
// nobody owed.
func TestTheThresholdIsExceededRatherThanReached(t *testing.T) {
	m := registry(t)
	f := facts(tds.IndividualNoAudit, tds.Resident, 2026, 4, 1)

	for _, c := range []struct {
		name    string
		monthly int64
		want    bool
	}{
		{"a rupee below", 49_999_00, false},
		{"exactly at the threshold", 50_000_00, false},
		{"a rupee above", 50_001_00, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, err := m.Assess(f, tds.Rent{MonthlyMinor: c.monthly, AnnualMinor: c.monthly * 12},
				effective.Day(2026, 5, 1))
			if err != nil {
				t.Fatalf("assessing: %v", err)
			}
			if a.Path.Section != tds.Section194IB {
				t.Fatalf("an individual below audit selected %s, want 194-IB", a.Path.Section)
			}
			if a.Deductible() != c.want {
				t.Errorf("%s: deductible = %v, want %v — %s", c.name, a.Deductible(), c.want, a.Because)
			}
			if a.Path.Timing != tds.OnceAtYearOrLeaseEnd {
				t.Errorf("194-IB timing is %s: a product that bills it monthly produces eleven wrong "+
					"challans and one surprise", a.Path.Timing)
			}
			if a.Path.RequiresTAN {
				t.Error("a 194-IB deductor was asked for a TAN, which is the one thing the section exists to avoid")
			}
		})
	}
}

// An assessment resolves the rule in force on the date it is asked about, so a
// deduction recomputed in November still reproduces March's numbers.
func TestAnAssessmentUsesTheRuleInForceOnItsOwnDate(t *testing.T) {
	m := registry(t)

	for _, c := range []struct {
		name          string
		on            effective.Date
		f             tds.Facts
		wantRate      int
		wantThreshold int64
	}{
		{"194-IB before the 2024 cut", effective.Day(2024, 9, 30),
			facts(tds.IndividualNoAudit, tds.Resident, 2020, 4, 1), 500, 50_000_00},
		{"194-IB after it", effective.Day(2024, 10, 1),
			facts(tds.IndividualNoAudit, tds.Resident, 2020, 4, 1), 200, 50_000_00},
		{"194-I before the 2025 threshold rise", effective.Day(2025, 3, 31),
			facts(tds.Business, tds.Resident, 2020, 4, 1), 1000, 24_000_00},
		{"194-I after it", effective.Day(2025, 4, 1),
			facts(tds.Business, tds.Resident, 2020, 4, 1), 1000, 6_00_000_00},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, err := m.Assess(c.f, tds.Rent{MonthlyMinor: 40_000_00, AnnualMinor: 4_80_000_00}, c.on)
			if err != nil {
				t.Fatalf("assessing: %v", err)
			}
			if a.RateBps != c.wantRate {
				t.Errorf("rate on %s is %d bps, want %d", c.on, a.RateBps, c.wantRate)
			}
			if a.ThresholdMinor != c.wantThreshold {
				t.Errorf("threshold on %s is %d, want %d", c.on, a.ThresholdMinor, c.wantThreshold)
			}
		})
	}
}

// The first edge case: a landlord whose residency changes mid-lease. The months
// before the change stay assessed as they were, because they were deducted,
// deposited and certified that way.
func TestResidencyChangingMidLeaseSplitsTheTenancyRatherThanRestatingIt(t *testing.T) {
	resident := facts(tds.Business, tds.Resident, 2026, 4, 1)
	left := facts(tds.Business, tds.NonResident, 2026, 10, 1)
	left.AcknowledgedOn, left.AcknowledgedBy = effective.Day(2026, 10, 1), "tenant:acme"

	h, err := tds.NewHistory([]effective.Record[tds.Facts]{
		{ID: "1", Range: between(t, 2026, 4, 1, 2026, 10, 1), Value: resident, Kind: effective.KindChange},
		{ID: "2", Range: since(t, 2026, 10, 1), Value: left, Kind: effective.KindChange},
	})
	if err != nil {
		t.Fatalf("building the history: %v", err)
	}

	for _, c := range []struct {
		on   effective.Date
		want tds.Section
	}{
		{effective.Day(2026, 4, 5), tds.Section194I},
		{effective.Day(2026, 9, 30), tds.Section194I},
		{effective.Day(2026, 10, 1), tds.Section195},
		{effective.Day(2027, 1, 5), tds.Section195},
	} {
		p, f, err := h.PathOn(c.on)
		if err != nil {
			t.Fatalf("resolving %s: %v", c.on, err)
		}
		if p.Section != c.want {
			t.Errorf("%s resolved to section %s, want %s", c.on, p.Section, c.want)
		}
		if err := p.RequireAcknowledgement(f); err != nil {
			t.Errorf("%s: %v", c.on, err)
		}
	}

	changes, err := h.Changes()
	if err != nil {
		t.Fatalf("listing changes: %v", err)
	}
	if len(changes) != 1 || !changes[0].Equal(effective.Day(2026, 10, 1)) {
		t.Errorf("the section changes on %v, want one change on 2026-10-01 — the lease screen has to "+
			"say this before the payout run discovers it", changes)
	}

	// Nothing is in force before the tenancy's first facts, and that is an error
	// rather than an assumption of residency.
	if _, _, err := h.PathOn(effective.Day(2026, 3, 31)); !errors.Is(err, tds.ErrFacts) {
		t.Errorf("a date before any recorded facts resolved anyway: %v", err)
	}
}

// The second edge case: joint owners whose shares fall below the threshold
// separately. That is the correct treatment where the shares are definite, and it
// is also the shape of the most common evasion, so the split has to be exact.
func TestJointOwnersAreAssessedOnTheirOwnSharesAndOnlyOnAnExactSplit(t *testing.T) {
	m := registry(t)
	f := facts(tds.Business, tds.Resident, 2026, 4, 1)
	rent := tds.Rent{MonthlyMinor: 90_000_00, AnnualMinor: 10_80_000_00}

	half := []tds.Payee{
		{Ref: "owner:a", Residency: tds.Resident, ShareBps: 5000,
			MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
		{Ref: "owner:b", Residency: tds.Resident, ShareBps: 5000,
			MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
	}

	// The whole rent is over the six-lakh threshold and each half is under it.
	whole, err := m.Assess(f, rent, effective.Day(2026, 5, 1))
	if err != nil {
		t.Fatalf("assessing the whole: %v", err)
	}
	if !whole.Deductible() {
		t.Fatalf("₹10,80,000 is under the threshold: %s", whole.Because)
	}

	split, err := m.Apportion(f, rent, half, effective.Day(2026, 5, 1))
	if err != nil {
		t.Fatalf("apportioning: %v", err)
	}
	for _, a := range split {
		if a.Deductible() {
			t.Errorf("a half share of ₹5,40,000 was assessed as deductible: %s", a.Because)
		}
	}

	// A co-owner abroad is on section 195 for their share, in the same month, out of
	// the same rent — and residency is per owner, never inherited from the lease.
	mixed := slices.Clone(half)
	mixed[1].Residency = tds.NonResident
	if _, err := m.Apportion(f, rent, mixed, effective.Day(2026, 5, 1)); !errors.Is(err, statutory.ErrNoRule) {
		t.Errorf("the non-resident co-owner's share resolved to a rate: %v", err)
	}

	// A split that does not add up is not an ascertainable share.
	for _, c := range []struct {
		name   string
		payees []tds.Payee
	}{
		{"shares that do not total ten thousand basis points", []tds.Payee{
			{Ref: "owner:a", Residency: tds.Resident, ShareBps: 5000,
				MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
		}},
		{"amounts that do not total the rent", []tds.Payee{
			{Ref: "owner:a", Residency: tds.Resident, ShareBps: 5000,
				MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
			{Ref: "owner:b", Residency: tds.Resident, ShareBps: 5000,
				MonthlyMinor: 44_999_99, AnnualMinor: 5_40_000_00},
		}},
		{"a co-owner named twice", []tds.Payee{
			{Ref: "owner:a", Residency: tds.Resident, ShareBps: 5000,
				MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
			{Ref: "owner:a", Residency: tds.Resident, ShareBps: 5000,
				MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
		}},
		{"a co-owner with no residency", []tds.Payee{
			{Ref: "owner:a", Residency: tds.Resident, ShareBps: 5000,
				MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
			{Ref: "owner:b", ShareBps: 5000,
				MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := m.Apportion(f, rent, c.payees, effective.Day(2026, 5, 1)); !errors.Is(err, tds.ErrShares) {
				t.Errorf("apportioned anyway: %v", err)
			}
		})
	}
}

// A government deductor deposits by book entry and reports on Form 24G. It never
// holds a challan number, and promising one is a reminder for a reference that
// will never exist.
func TestAGovernmentDeductorDepositsByBookEntry(t *testing.T) {
	p, err := tds.Select(facts(tds.Government, tds.Resident, 2026, 4, 1))
	if err != nil {
		t.Fatalf("selecting: %v", err)
	}
	if !slices.Contains(p.Artefacts, tds.BookEntry24G) || slices.Contains(p.Artefacts, tds.Challan) {
		t.Errorf("a government deductor's artefacts are %v", p.Artefacts)
	}

	// And the specialisation does not leak into the shared table.
	other, err := tds.Select(facts(tds.Business, tds.Resident, 2026, 4, 1))
	if err != nil {
		t.Fatalf("selecting: %v", err)
	}
	if !slices.Contains(other.Artefacts, tds.Challan) {
		t.Errorf("a business deductor's artefacts are %v — the government path mutated the shared "+
			"path rather than copying it", other.Artefacts)
	}
}

// Facts that cannot select a path are refused where they are entered, rather than
// at the payout run nine months later.
func TestIncompleteFactsAreRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		f    tds.Facts
	}{
		{"no deductor class", tds.Facts{Residency: tds.Resident, From: effective.Day(2026, 4, 1)}},
		{"no residency", tds.Facts{Deductor: tds.Business, From: effective.Day(2026, 4, 1)}},
		{"no date", tds.Facts{Deductor: tds.Business, Residency: tds.Resident}},
		{"an anonymous acknowledgement", tds.Facts{Deductor: tds.Business, Residency: tds.NonResident,
			From: effective.Day(2026, 4, 1), AcknowledgedOn: effective.Day(2026, 4, 1)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := tds.Select(c.f); !errors.Is(err, tds.ErrFacts) {
				t.Errorf("selected a path anyway: %v", err)
			}
		})
	}
}

// The matrix's vocabulary is the registry's. A rename in statutory that this
// package did not follow is a gap at the payout run, so it is a failure here.
func TestEveryPathResolvesAgainstTheRegistry(t *testing.T) {
	m := registry(t)
	on := effective.Day(2026, 5, 1)

	for _, s := range tds.Sections() {
		p, ok := tds.PathFor(s)
		if !ok {
			t.Fatalf("section %s has no path", s)
		}
		if s == tds.Section195 {
			// Deliberately absent, and asserted absent: the day somebody adds a section
			// 195 rate row this test says so, and the ADR's reasoning gets revisited.
			continue
		}
		f := facts(tds.Business, tds.Resident, 2026, 4, 1)
		if s == tds.Section194IB {
			f.Deductor = tds.IndividualNoAudit
		}
		a, err := m.Assess(f, tds.Rent{MonthlyMinor: 40_000_00, AnnualMinor: 4_80_000_00}, on)
		if err != nil {
			t.Fatalf("section %s does not resolve: %v", s, err)
		}
		if a.Path.RateKey != p.RateKey || a.Path.ThresholdKey != p.ThresholdKey {
			t.Errorf("section %s resolved keys %s/%s", s, a.Path.RateKey, a.Path.ThresholdKey)
		}
		if a.Verification != statutory.NeedsBareActCheck || a.Enforcement != statutory.Warn {
			t.Errorf("section %s reports %s/%s, and both rows it used are needs_bare_act_check/warn — "+
				"an assessment may not be more confident than the weaker row it rests on",
				s, a.Verification, a.Enforcement)
		}
	}
}
