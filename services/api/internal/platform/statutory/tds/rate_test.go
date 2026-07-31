package tds_test

import (
	"errors"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

// ADR-0025: the rate a deductor actually deducts at, which is not the section's.

func certificate(t *testing.T, s tds.Section, bps int, iv effective.Interval) *tds.Certificate {
	t.Helper()
	return &tds.Certificate{
		Number: "BLR/197/2026/0042", Section: s, RateBps: bps,
		Validity: iv, IssuedOn: iv.From(),
	}
}

// The one that unlocks section 195. The registry holds no rate, so an assessment
// fails — until an Assessing Officer has determined one, at which point the
// certificate *is* the rate.
func TestASection197CertificateIsWhatMakesASection195DeductionComputable(t *testing.T) {
	m := registry(t)
	f := facts(tds.Business, tds.NonResident, 2026, 4, 1)
	rent := tds.Rent{MonthlyMinor: 60_000_00, AnnualMinor: 7_20_000_00}
	on := effective.Day(2026, 5, 7)

	if _, err := m.Assess(f, onFile("owner:nri"), rent, on); !errors.Is(err, statutory.ErrNoRule) {
		t.Fatalf("section 195 resolved to a rate with no certificate: %v", err)
	}

	held := onFile("owner:nri")
	held.Certificate = certificate(t, tds.Section195, 500, between(t, 2026, 4, 1, 2027, 4, 1))

	a, err := m.Assess(f, held, rent, on)
	if err != nil {
		t.Fatalf("assessing with a certificate: %v", err)
	}
	if a.RateBps != 500 {
		t.Errorf("deducting at %d bps, want the certificate's 500", a.RateBps)
	}
	if !a.Deductible() {
		t.Error("section 195 stopped being deductible from the first rupee")
	}
	if len(a.Rate.Applied) != 1 || a.Rate.Applied[0].Under != "197" {
		t.Fatalf("the trail is %+v, want one step under section 197", a.Rate.Applied)
	}

	// Surcharge and cess are not stacked on a rate an officer determined.
	for _, step := range a.Rate.Applied {
		if step.Under == "surcharge" || step.Under == "cess" {
			t.Errorf("a section 197 rate was raised by %s — the officer determined the rate to "+
				"deduct at, not a base to build on", step.Under)
		}
	}

	// A nil certificate is a real outcome and deducts nothing, without becoming a
	// missing rate.
	nil197 := onFile("owner:nri")
	nil197.Certificate = certificate(t, tds.Section195, 0, between(t, 2026, 4, 1, 2027, 4, 1))
	a, err = m.Assess(f, nil197, rent, on)
	if err != nil {
		t.Fatalf("assessing a nil certificate: %v", err)
	}
	if a.RateBps != 0 {
		t.Errorf("a nil-deduction certificate deducted at %d bps", a.RateBps)
	}
}

// A certificate is issued for a section and for a period, and outside either it
// does nothing.
func TestACertificateIsBoundedBySectionAndByDate(t *testing.T) {
	m := registry(t)
	f := facts(tds.Business, tds.Resident, 2026, 4, 1)
	rent := tds.Rent{MonthlyMinor: 80_000_00, AnnualMinor: 9_60_000_00}

	for _, c := range []struct {
		name    string
		cert    *tds.Certificate
		on      effective.Date
		wantBps int
	}{
		{"in force", certificate(t, tds.Section194I, 300, between(t, 2026, 4, 1, 2027, 4, 1)),
			effective.Day(2026, 6, 1), 300},
		{"expired", certificate(t, tds.Section194I, 300, between(t, 2025, 4, 1, 2026, 4, 1)),
			effective.Day(2026, 6, 1), 1000},
		{"not yet in force", certificate(t, tds.Section194I, 300, between(t, 2026, 7, 1, 2027, 4, 1)),
			effective.Day(2026, 6, 1), 1000},
		{"issued for another section", certificate(t, tds.Section195, 300, between(t, 2026, 4, 1, 2027, 4, 1)),
			effective.Day(2026, 6, 1), 1000},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := onFile("owner:a")
			p.Certificate = c.cert
			a, err := m.Assess(f, p, rent, c.on)
			if err != nil {
				t.Fatalf("assessing: %v", err)
			}
			if a.RateBps != c.wantBps {
				t.Errorf("deducting at %d bps, want %d — %+v", a.RateBps, c.wantBps, a.Rate.Applied)
			}
		})
	}
}

// Section 206AA, and the zero value that has to deduct more rather than less.
func TestNoPANDeductsAtTheSection206AAFloor(t *testing.T) {
	m := registry(t)
	f := facts(tds.Business, tds.Resident, 2026, 4, 1)
	rent := tds.Rent{MonthlyMinor: 80_000_00, AnnualMinor: 9_60_000_00}
	on := effective.Day(2026, 6, 1)

	// The zero profile: nobody said whether a PAN is on file. It deducts at 20%,
	// not at 10% — a default that under-deducts is the failure nobody notices.
	a, err := m.Assess(f, tds.PayeeProfile{Ref: "owner:unknown"}, rent, on)
	if err != nil {
		t.Fatalf("assessing: %v", err)
	}
	if a.RateBps != 2000 {
		t.Fatalf("a payee with no PAN on file deducted at %d bps, want the 2000 floor", a.RateBps)
	}
	if a.Rate.SectionBps != 1000 {
		t.Errorf("the section's own rate is reported as %d bps", a.Rate.SectionBps)
	}
	if len(a.Rate.Applied) != 1 || a.Rate.Applied[0].Under != "206AA" {
		t.Errorf("the trail is %+v, want one step under 206AA", a.Rate.Applied)
	}
	if a.Rate.Applied[0].RuleID == "" {
		t.Error("the 206AA step does not name the row that supplied the floor")
	}

	// A resident has no rule 37BC escape — it is a non-resident provision, and a
	// resident who ticks it stays on the floor.
	stillFloored := tds.PayeeProfile{Ref: "owner:unknown", Rule37BCFurnished: true}
	if a, err = m.Assess(f, stillFloored, rent, on); err != nil {
		t.Fatalf("assessing: %v", err)
	} else if a.RateBps != 2000 {
		t.Errorf("a resident escaped section 206AA through rule 37BC, at %d bps", a.RateBps)
	}
}

// Rule 37BC: a non-resident who furnishes a tax residency certificate, a TIN and
// contact details is outside section 206AA — which matters because without it
// every NRI landlord without an Indian PAN is at 20% before surcharge.
func TestRule37BCTakesANonResidentOutOfSection206AA(t *testing.T) {
	// Against the counterfactual registry, because rule 37BC is about what section
	// 206AA adds to a known rate, and section 195's rate is deliberately unknown.
	m := registryWithSection195Rate(t)
	f := facts(tds.Business, tds.NonResident, 2026, 4, 1)
	rent := tds.Rent{MonthlyMinor: 60_000_00, AnnualMinor: 7_20_000_00}
	on := effective.Day(2026, 5, 7)

	noPAN := tds.PayeeProfile{Ref: "owner:nri", Form: tds.FormIndividual}
	a, err := m.Assess(f, noPAN, rent, on)
	if err != nil {
		t.Fatalf("assessing: %v", err)
	}
	// 20% floor, then 4% cess on it — no surcharge under fifty lakh.
	if a.RateBps != 2080 {
		t.Errorf("an NRI with no PAN deducted at %d bps, want 2080 — the 206AA floor plus cess. %+v",
			a.RateBps, a.Rate.Applied)
	}

	// With the particulars furnished, section 206AA falls away and the section's
	// own rate stands: 10% plus cess.
	furnished := noPAN
	furnished.Rule37BCFurnished = true
	b, err := m.Assess(f, furnished, rent, on)
	if err != nil {
		t.Fatalf("assessing: %v", err)
	}
	if b.RateBps != 1040 {
		t.Errorf("with rule 37BC furnished the rate is %d bps, want 1040", b.RateBps)
	}
}

// Section 206AA is a floor over a known rate, not a rate of its own. Where the
// section's rate is a gap — section 195, today — a missing PAN does not turn 20%
// into an answer, because "the higher of twenty per cent and an unknown number"
// is not twenty per cent.
func TestTheSection206AAFloorDoesNotRescueAMissingSectionRate(t *testing.T) {
	m := registry(t)
	f := facts(tds.Business, tds.NonResident, 2026, 4, 1)

	_, err := m.Assess(f, tds.PayeeProfile{Ref: "owner:nri"},
		tds.Rent{MonthlyMinor: 60_000_00, AnnualMinor: 7_20_000_00}, effective.Day(2026, 5, 7))
	if !errors.Is(err, statutory.ErrNoRule) {
		t.Fatalf("a missing section 195 rate resolved to the 206AA floor: %v", err)
	}
}

// Section 206AB existed for three years and three months. A deduction dated
// inside that window resolves it; one dated after does not, because the section
// was omitted rather than reduced to nil.
func TestSection206ABAppliesOnlyWhileItExisted(t *testing.T) {
	m := registry(t)
	f := facts(tds.Business, tds.Resident, 2023, 4, 1)
	rent := tds.Rent{MonthlyMinor: 80_000_00, AnnualMinor: 9_60_000_00}
	nonFiler := onFile("owner:a")
	nonFiler.SpecifiedNonFiler = true

	for _, c := range []struct {
		name string
		on   effective.Date
		want int
	}{
		// Twice the section's ten per cent beats the five per cent floor.
		{"while the section was in force", effective.Day(2024, 9, 30), 2000},
		{"the day it was omitted", effective.Day(2024, 10, 1), 1000},
		{"long after", effective.Day(2026, 6, 1), 1000},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, err := m.Assess(f, nonFiler, rent, c.on)
			if err != nil {
				t.Fatalf("assessing: %v", err)
			}
			if a.RateBps != c.want {
				t.Errorf("on %s a specified non-filer deducted at %d bps, want %d — %+v",
					c.on, a.RateBps, c.want, a.Rate.Applied)
			}
		})
	}

	// Both provisions at once: the higher of the two, per section 206AB(2).
	both := tds.PayeeProfile{Ref: "owner:a", SpecifiedNonFiler: true}
	a, err := m.Assess(f, both, rent, effective.Day(2024, 9, 30))
	if err != nil {
		t.Fatalf("assessing: %v", err)
	}
	if a.RateBps != 2000 {
		t.Errorf("a non-filer with no PAN deducted at %d bps, want the higher of the two floors", a.RateBps)
	}
}

// Surcharge and cess, and the reason the effective rate is not in any table of
// sections: a ₹10,000 flat and a ₹60,00,000 penthouse let by the same NRI are
// deducted at different rates.
func TestANonResidentPaysSurchargeAndCessOnTopOfTheSectionRate(t *testing.T) {
	m := registry(t)
	f := facts(tds.Business, tds.NonResident, 2026, 4, 1)
	on := effective.Day(2026, 5, 7)

	// The section 195 rate is a gap, so this is asserted through a resident whose
	// section rate is known — the composition is what is being tested, and it is
	// the same arithmetic on either path.
	resident := facts(tds.Business, tds.Resident, 2026, 4, 1)
	a, err := m.Assess(resident, onFile("owner:res"), tds.Rent{MonthlyMinor: 8_00_000_00,
		AnnualMinor: 96_00_000_00}, on)
	if err != nil {
		t.Fatalf("assessing a resident: %v", err)
	}
	if a.RateBps != 1000 || a.Rate.Overridden() {
		t.Errorf("a resident earning ₹96 lakh was charged surcharge or cess: %d bps, %+v",
			a.RateBps, a.Rate.Applied)
	}

	// The same rent to a non-resident, under a certificate at the section rate so
	// there is a base to compose on. 10% + 10% surcharge = 11%, + 4% cess = 11.44%.
	nri := onFile("owner:nri")
	cert := registryWithSection195Rate(t)
	b, err := cert.Assess(f, nri, tds.Rent{MonthlyMinor: 8_00_000_00, AnnualMinor: 96_00_000_00}, on)
	if err != nil {
		t.Fatalf("assessing a non-resident: %v", err)
	}
	if b.RateBps != 1144 {
		t.Fatalf("deducted at %d bps, want 1144 — 10%% raised by a 10%% surcharge and 4%% cess. %+v",
			b.RateBps, b.Rate.Applied)
	}
	if len(b.Rate.Applied) != 2 ||
		b.Rate.Applied[0].Under != "surcharge" || b.Rate.Applied[1].Under != "cess" {
		t.Errorf("the trail is %+v, want surcharge then cess", b.Rate.Applied)
	}

	// Below the surcharge threshold only cess applies: 10% + 4% = 10.4%.
	small, err := cert.Assess(f, nri, tds.Rent{MonthlyMinor: 60_000_00, AnnualMinor: 7_20_000_00}, on)
	if err != nil {
		t.Fatalf("assessing: %v", err)
	}
	if small.RateBps != 1040 {
		t.Errorf("a ₹7.2 lakh year deducted at %d bps, want 1040 — under the surcharge threshold, "+
			"cess still applies", small.RateBps)
	}

	// A foreign company is on the flatter ladder: nil surcharge below a crore.
	company := nri
	company.Form = tds.FormCompany
	co, err := cert.Assess(f, company, tds.Rent{MonthlyMinor: 8_00_000_00, AnnualMinor: 96_00_000_00}, on)
	if err != nil {
		t.Fatalf("assessing a foreign company: %v", err)
	}
	if co.RateBps != 1040 {
		t.Errorf("a foreign company under a crore deducted at %d bps, want 1040 — its surcharge "+
			"threshold is a crore, not fifty lakh", co.RateBps)
	}
}

// A certificate held by a payee with no PAN is two records disagreeing, because
// section 206AA(4) forbids issuing one without a PAN. Guessing which is wrong is
// worse than saying so.
func TestACertificateWithoutAPANIsRefused(t *testing.T) {
	m := registry(t)
	p := tds.PayeeProfile{Ref: "owner:a"}
	p.Certificate = certificate(t, tds.Section194I, 300, between(t, 2026, 4, 1, 2027, 4, 1))

	_, err := m.Assess(facts(tds.Business, tds.Resident, 2026, 4, 1), p,
		tds.Rent{MonthlyMinor: 80_000_00, AnnualMinor: 9_60_000_00}, effective.Day(2026, 6, 1))
	if !errors.Is(err, tds.ErrFacts) {
		t.Fatalf("a certificate held without a PAN was applied: %v", err)
	}
}

// Co-owners carry their own PAN, certificate and filer status. One owner's
// certificate does not lower the deduction on the other's share.
func TestOverridesArePerCoOwner(t *testing.T) {
	m := registry(t)
	f := facts(tds.Business, tds.Resident, 2026, 4, 1)
	rent := tds.Rent{MonthlyMinor: 90_000_00, AnnualMinor: 10_80_000_00}

	a := onFile("owner:a")
	a.Certificate = certificate(t, tds.Section194I, 200, between(t, 2026, 4, 1, 2027, 4, 1))

	out, err := m.Apportion(f, rent, []tds.Payee{
		{Ref: "owner:a", Residency: tds.Resident, ShareBps: 5000,
			MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00, Profile: a},
		{Ref: "owner:b", Residency: tds.Resident, ShareBps: 5000,
			MonthlyMinor: 45_000_00, AnnualMinor: 5_40_000_00},
	}, effective.Day(2026, 6, 1))
	if err != nil {
		t.Fatalf("apportioning: %v", err)
	}
	if out[0].RateBps != 200 {
		t.Errorf("the certificate holder's share deducted at %d bps, want 200", out[0].RateBps)
	}
	// Owner B has no profile at all, so no PAN is on file: the floor, not the
	// certificate, and not the section rate either.
	if out[1].RateBps != 2000 {
		t.Errorf("the other co-owner deducted at %d bps, want the 206AA floor — a share with no "+
			"PAN on file must not inherit anything from the co-owner", out[1].RateBps)
	}
}

// registryWithSection195Rate is the counterfactual: what the matrix does once a
// section 195 rate exists. It is a fixture and not a seed — ADR-0024 §5 is why
// the schema holds no such row.
func registryWithSection195Rate(t *testing.T) *tds.Matrix {
	t.Helper()
	m := registry(t)
	rules := append(m.Registry().Rules(), statutory.Rule{
		ID: "fixture-195", Type: statutory.TDSRate, Jurisdiction: statutory.National,
		Key: "tds.195_non_resident", Kind: statutory.KindRate, RateBps: 1000,
		Validity: since(t, 2020, 4, 1), StatuteRef: "fixture — see ADR-0024 §5",
		Verification: statutory.Unverified, Owner: "compliance",
		ReviewDue: effective.Day(2027, 1, 1), Enforcement: statutory.RecordOnly,
	})
	table, err := statutory.NewTable(rules)
	if err != nil {
		t.Fatalf("building the counterfactual registry: %v", err)
	}
	out, err := tds.New(table)
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	return out
}
