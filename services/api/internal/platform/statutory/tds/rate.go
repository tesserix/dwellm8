package tds

import (
	"errors"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
)

// The rate the deductor actually deducts at, which is not the section's rate.
// ADR-0025.
//
// Four things move it, and they move it for facts about the *payee* rather than
// about the payment: whether their PAN is on file (§206AA), whether they were a
// specified non-filer while that section existed (§206AB), whether an Assessing
// Officer has issued them a certificate (§197), and — for a non-resident only —
// surcharge and cess on top.
//
// They are applied in one documented order, and every step is recorded. An
// effective rate a deductor cannot explain line by line is one they cannot defend
// when the payee asks why they received less than the agreed rent.

// PayeeForm is the payee's own legal form, which decides which surcharge ladder
// they are on. Only consulted for a non-resident.
type PayeeForm string

const (
	// FormIndividual covers an individual, HUF, AOP or BOI.
	FormIndividual PayeeForm = "individual"
	// FormCompany is a foreign company, on a much flatter ladder.
	FormCompany PayeeForm = "company"
)

// Certificate is a section 197 lower or nil deduction certificate: an Assessing
// Officer's determination that this payee's tax on this income is less than the
// section would deduct.
//
// It names a section, because one issued for §194-I does not lower a §195
// deduction, and a nil certificate for rent applied to a capital payment is the
// mistake that makes that worth saying.
type Certificate struct {
	Number   string
	Section  Section
	RateBps  int
	Validity effective.Interval
	IssuedOn effective.Date
}

// Valid reports whether the certificate covers this section on this date.
func (c Certificate) Valid(s Section, on effective.Date) bool {
	return c.Number != "" && c.Section == s && c.Validity.Valid() && c.Validity.Contains(on)
}

// PayeeProfile is what is known about the landlord for rate purposes.
//
// The zero value deducts *more*, not less: no PAN on file means §206AA's floor,
// and that is the safe direction for a field nobody filled in. A profile that
// defaulted to "PAN held" would under-deduct silently, which is the failure this
// package exists to prevent.
type PayeeProfile struct {
	Ref  string
	Form PayeeForm

	// HasPAN is whether the payee has furnished a PAN. Not whether they have one —
	// §206AA turns on furnishing it to the deductor.
	HasPAN bool

	// Rule37BCFurnished is the non-resident's escape from §206AA: name, email,
	// phone, address, tax residency certificate and TIN. It does nothing for a
	// resident, and this struct does not pretend otherwise — the resolution ignores
	// it unless the payee is a non-resident.
	Rule37BCFurnished bool

	// SpecifiedNonFiler is §206AB's trigger. The section was omitted with effect
	// from 1 October 2024, so this can only raise a rate for a date before that —
	// which is why the floor is a bounded row rather than deleted.
	SpecifiedNonFiler bool

	// Certificate is the §197 certificate, if one is held.
	Certificate *Certificate
}

// Validate refuses a profile that cannot be reasoned about.
func (p PayeeProfile) Validate() error {
	switch {
	case p.Ref == "":
		return fmt.Errorf("%w: a payee with no identity", ErrFacts)
	case p.Form != "" && p.Form != FormIndividual && p.Form != FormCompany:
		return fmt.Errorf("%w: %q is not a payee form", ErrFacts, p.Form)
	case p.Certificate != nil && !p.HasPAN:
		// §206AA(4): no certificate under §197 shall be granted unless the
		// application carries the PAN. A certificate held by a payee with no PAN on
		// file is one of the two records being wrong, and guessing which is worse
		// than saying so.
		return fmt.Errorf("%w: %s holds a section 197 certificate and no PAN — a certificate "+
			"cannot be issued without one, so one of the two records is wrong", ErrFacts, p.Ref)
	case p.Certificate != nil && p.Certificate.RateBps < 0:
		return fmt.Errorf("%w: certificate %s carries a negative rate", ErrFacts, p.Certificate.Number)
	}
	return nil
}

// Adjustment is one step in getting from the section's rate to the deducted one.
type Adjustment struct {
	// Under is the provision that moved it: "197", "206AA", "206AB", "surcharge",
	// "cess".
	Under string
	// FromBps and ToBps are the rate either side of this step.
	FromBps, ToBps int
	// RuleID is the registry row that supplied the number, where one did. Empty for
	// a step driven by a certificate or by statutory arithmetic.
	RuleID string
	// Because is the sentence shown to whoever asks why the rent arrived short.
	Because string
}

// EffectiveRate is the rate to deduct at, and how it got there.
type EffectiveRate struct {
	// SectionBps is the section's own rate, before anything about the payee.
	SectionBps int
	// Bps is what is actually deducted at.
	Bps int
	// Applied is every step, in the order it was applied. Empty when the section's
	// rate stood unchanged, which is the ordinary case.
	Applied []Adjustment
}

// Overridden reports whether anything moved the section's rate.
func (e EffectiveRate) Overridden() bool { return len(e.Applied) > 0 }

// certificateRate is set when a §197 certificate decided the rate, which
// suppresses surcharge and cess: the officer determined the rate to deduct at,
// not a base to build on.
func (m *Matrix) effectiveRate(
	p Path, f Facts, profile PayeeProfile, rent Rent, on effective.Date,
) (EffectiveRate, error) {
	if err := profile.Validate(); err != nil {
		return EffectiveRate{}, err
	}

	var (
		out      EffectiveRate
		baseRule string
		haveBase bool
	)

	// The section's own rate. A §195 gap is survivable here and only here: a
	// certificate is a rate in its own right, and it is the whole reason §195 is
	// computable at all.
	base, err := m.rules.Resolve(ruleTypes.Rate, statutory.National, p.RateKey, on)
	switch {
	case err == nil:
		if out.SectionBps, err = base.Rule.Rate(); err != nil {
			return EffectiveRate{}, err
		}
		out.Bps, baseRule, haveBase = out.SectionBps, base.Rule.ID, true
	case errors.Is(err, statutory.ErrNoRule) && profile.Certificate != nil &&
		profile.Certificate.Valid(p.Section, on):
		// Fall through to the certificate below.
	default:
		return EffectiveRate{}, fmt.Errorf("tds: section %s has no rate to deduct at: %w", p.Section, err)
	}

	if c := profile.Certificate; c != nil && c.Valid(p.Section, on) {
		out.Applied = append(out.Applied, Adjustment{
			Under: "197", FromBps: out.Bps, ToBps: c.RateBps,
			Because: fmt.Sprintf("Certificate %s under section 197 sets the rate for section %s "+
				"until %s. Surcharge and cess are not added to a rate an officer determined.",
				c.Number, p.Section, c.Validity.To()),
		})
		out.Bps = c.RateBps
		return out, nil
	}

	if !haveBase {
		return EffectiveRate{}, fmt.Errorf("tds: section %s has no rate to deduct at: %w",
			p.Section, &statutory.Gap{Type: ruleTypes.Rate, Jurisdiction: statutory.National,
				Key: p.RateKey, On: on})
	}

	// §206AA. A non-resident who has furnished rule 37BC's particulars is outside
	// it; a resident has no such escape.
	exempt37BC := f.Residency == NonResident && profile.Rule37BCFurnished
	if !profile.HasPAN && !exempt37BC {
		floor, err := m.rules.Resolve(ruleTypes.Rate, statutory.National, "tds.206aa_no_pan_floor", on)
		if err != nil {
			return EffectiveRate{}, fmt.Errorf("tds: %s has furnished no PAN and section 206AA's "+
				"floor is not in the registry: %w", profile.Ref, err)
		}
		bps, err := floor.Rule.Rate()
		if err != nil {
			return EffectiveRate{}, err
		}
		if bps > out.Bps {
			out.Applied = append(out.Applied, Adjustment{
				Under: "206AA", FromBps: out.Bps, ToBps: bps, RuleID: floor.Rule.ID,
				Because: fmt.Sprintf("%s has not furnished a PAN, so section 206AA deducts at the "+
					"higher of the section rate and %s.", profile.Ref, pct(bps)),
			})
			out.Bps = bps
		}
	}

	// §206AB, for as long as it existed. Twice the section's rate or five per cent,
	// whichever is higher — and where §206AA also applies, the higher of the two,
	// which taking the maximum at each step already gives.
	if profile.SpecifiedNonFiler {
		floor, err := m.rules.Resolve(ruleTypes.Rate, statutory.National, "tds.206ab_non_filer_floor", on)
		switch {
		case errors.Is(err, statutory.ErrNoRule):
			// The section was omitted with effect from 1 October 2024. Nothing to
			// apply, and nothing wrong: a payee flagged as a non-filer after that date
			// simply is not subject to it.
		case err != nil:
			return EffectiveRate{}, err
		default:
			bps, err := floor.Rule.Rate()
			if err != nil {
				return EffectiveRate{}, err
			}
			if twice := out.SectionBps * 2; twice > bps {
				bps = twice
			}
			if bps > out.Bps {
				out.Applied = append(out.Applied, Adjustment{
					Under: "206AB", FromBps: out.Bps, ToBps: bps, RuleID: floor.Rule.ID,
					Because: fmt.Sprintf("%s was a specified non-filer, so section 206AB deducts at "+
						"the higher of twice the section rate and the statutory floor.", profile.Ref),
				})
				out.Bps = bps
			}
		}
	}

	if f.Residency != NonResident {
		// Surcharge and cess do not apply to a resident's non-salary TDS. The section
		// rate is the rate, and adding four per cent of cess to it is a common and
		// expensive error in the other direction.
		_ = baseRule
		return out, nil
	}
	return m.applySurchargeAndCess(out, profile, rent, on)
}

// applySurchargeAndCess raises a non-resident's rate by the surcharge band its
// payment falls in and then by cess on the total.
//
// The band is selected on the year's aggregate to this payee, which is the figure
// the surcharge thresholds are written against. Both compose on the rate rather
// than on the money: a rate is a ratio, and the money module still performs the
// single multiplication that turns it into paise.
func (m *Matrix) applySurchargeAndCess(
	out EffectiveRate, profile PayeeProfile, rent Rent, on effective.Date,
) (EffectiveRate, error) {
	form := profile.Form
	if form == "" {
		form = FormIndividual
	}
	key := "tds.surcharge.non_resident_individual"
	if form == FormCompany {
		key = "tds.surcharge.foreign_company"
	}

	scale, err := m.rules.Resolve(ruleTypes.Surcharge, statutory.National, key, on)
	if err != nil {
		return EffectiveRate{}, fmt.Errorf("tds: a payment to a non-resident has no surcharge scale "+
			"to read: %w", err)
	}
	band, err := scale.Rule.Band(rent.AnnualMinor)
	if err != nil {
		return EffectiveRate{}, err
	}
	if band.RateBps > 0 {
		raised, err := compose(out.Bps, band.RateBps)
		if err != nil {
			return EffectiveRate{}, err
		}
		out.Applied = append(out.Applied, Adjustment{
			Under: "surcharge", FromBps: out.Bps, ToBps: raised, RuleID: scale.Rule.ID,
			Because: fmt.Sprintf("A %s payee receiving %s in the year falls in the %s surcharge band.",
				form, paise(rent.AnnualMinor), pct(band.RateBps)),
		})
		out.Bps = raised
	}

	cess, err := m.rules.Resolve(ruleTypes.Cess, statutory.National, "tds.cess.health_and_education", on)
	if err != nil {
		return EffectiveRate{}, fmt.Errorf("tds: a payment to a non-resident has no cess rate to "+
			"read: %w", err)
	}
	cessBps, err := cess.Rule.Rate()
	if err != nil {
		return EffectiveRate{}, err
	}
	if cessBps > 0 {
		raised, err := compose(out.Bps, cessBps)
		if err != nil {
			return EffectiveRate{}, err
		}
		out.Applied = append(out.Applied, Adjustment{
			Under: "cess", FromBps: out.Bps, ToBps: raised, RuleID: cess.Rule.ID,
			Because: fmt.Sprintf("Health and education cess at %s on tax and surcharge together.",
				pct(cessBps)),
		})
		out.Bps = raised
	}
	return out, nil
}

// compose raises a rate by a percentage of itself: 1000 bps raised by 1000 bps of
// surcharge is 1100.
//
// Not money, so not ADR-0007's allocator — but it divides, so it rounds, and it
// rounds half away from zero for the same reason that one does. Both operands are
// bounded by the registry's own CHECK at 1,000,000 bps, so the product cannot
// approach the int64 range.
func compose(bps, byBps int) (int, error) {
	if bps < 0 || byBps < 0 {
		return 0, fmt.Errorf("tds: composing %d bps with %d bps", bps, byBps)
	}
	n := int64(bps) * int64(10000+byBps)
	out := (n + 5000) / 10000
	if out > 1_000_000 {
		return 0, fmt.Errorf("tds: %s raised by %s exceeds one hundred per cent",
			pct(bps), pct(byBps))
	}
	return int(out), nil
}

// pct formats basis points for a human: 1000 is "10%", 250 is "2.5%".
func pct(bps int) string {
	if bps%100 == 0 {
		return fmt.Sprintf("%d%%", bps/100)
	}
	return fmt.Sprintf("%d.%02d%%", bps/100, bps%100)
}
