// Package tds is the decision matrix for tax deducted at source on rent:
// which section applies, at whose risk, and what paperwork it costs. ADR-0024.
//
// Two facts decide everything — what kind of payer the tenant is, and whether the
// landlord is a resident. Section 194-I, section 194-IB and section 195 differ in
// rate, in threshold, in periodicity, in the forms they produce and in who is
// ruined when they are missed, and the difference between them is settled by those
// two facts alone. So they are captured when the lease is written, not discovered
// at payout: a tenancy nine months old that turns out to have been a section 195
// tenancy all along owes nine months of deduction, interest and penalty, and the
// platform that never asked is the reason.
//
// # The obligation is the deductor's
//
// Nothing here transfers it. The deductor — the tenant, in every path — deducts,
// deposits, files and certifies; Dwellm8 computes, reminds, records and produces
// references. That is a legal fact rather than a disclaimer, and it is the reason
// this package selects and never files. See Facilitation.
//
// # Selection is free, arithmetic is not
//
// Select answers which section applies without touching the registry, so a lease
// screen can say "section 195, from the first rupee, and you carry it" for a
// jurisdiction whose rate nobody has verified yet. Assess is what needs numbers,
// and it fails with a *statutory.Gap rather than a default when the rate is not in
// the registry — which is exactly where section 195 sits today, because its rate
// is a treaty question and there is no single number to hold.
//
// # No money is computed here
//
// Assess returns a rate in basis points and whether the threshold is crossed. It
// does not multiply it by anything. ADR-0007 permits one rounding primitive in the
// platform and it lives under internal/money; a second one here would be a second
// answer to "which way does a half-paisa go", so the money module applies the rate
// this package selects.
package tds

import (
	"errors"
	"fmt"
	"slices"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
)

// Facilitation is what the product says about its own role, in the terms of
// service, on the lease screen and in this package's godoc, in the same words.
//
// It is a constant rather than a paragraph three teams paraphrase differently.
const Facilitation = "The deduction, deposit, return and certificate are the deductor's obligation. " +
	"Dwellm8 computes the deduction, records what was deposited and produces references; " +
	"it does not deduct, deposit or file on anyone's behalf."

// DeductorClass is what kind of payer the tenant is, in the only terms the
// Income-tax Act cares about.
//
// The distinction that matters is narrower than "individual versus company": it is
// whether the payer is an individual or HUF *not* liable to audit under section
// 44AB, because that class alone deducts under section 194-IB and everyone else
// deducts under section 194-I. A salaried tenant and a doctor with a large practice
// are both individuals and they are not on the same path.
type DeductorClass string

const (
	// IndividualNoAudit is an individual or HUF not liable to tax audit — the
	// salaried tenant, the small proprietor. Section 194-IB.
	IndividualNoAudit DeductorClass = "individual_no_audit"

	// IndividualAudited is an individual or HUF liable to audit under section 44AB.
	// Section 194-I, with everything that follows from it: a TAN, quarterly returns,
	// and a class of tenant who is usually astonished to learn it.
	IndividualAudited DeductorClass = "individual_audited"

	// Business is a company, firm, LLP, AOP or BOI. Section 194-I.
	Business DeductorClass = "business"

	// Government is a government department or local authority. Section 194-I, and
	// held apart from Business because its deposit mechanics differ — book entry
	// through Form 24G rather than a bank challan — and the artefact list has to say
	// so rather than promise a challan number that will never exist.
	Government DeductorClass = "government"
)

// DeductorClasses is every class, ordered, for a form and for the schema contract
// test.
func DeductorClasses() []DeductorClass {
	return []DeductorClass{IndividualNoAudit, IndividualAudited, Business, Government}
}

func (d DeductorClass) Valid() bool { return slices.Contains(DeductorClasses(), d) }

// Residency is the landlord's residential status for the year in question.
//
// Not their nationality and not where they live today: an Indian citizen working
// abroad is a non-resident, and a foreign national who has been here long enough is
// a resident. The product records the answer and the date it was given; it does not
// compute the day count, which is the landlord's tax position and not ours.
type Residency string

const (
	Resident    Residency = "resident"
	NonResident Residency = "non_resident"
)

// Residencies is both values, ordered.
func Residencies() []Residency { return []Residency{Resident, NonResident} }

func (r Residency) Valid() bool { return slices.Contains(Residencies(), r) }

// Section is the section of the Income-tax Act 1961 under which the deduction is
// made.
type Section string

const (
	Section194I  Section = "194i"
	Section194IB Section = "194ib"
	Section195   Section = "195"
)

// Sections is every section this matrix can select, ordered.
func Sections() []Section { return []Section{Section194I, Section194IB, Section195} }

// Basis is what the threshold is tested against.
type Basis string

const (
	// BasisAnnual tests the aggregate paid to one payee in the financial year.
	BasisAnnual Basis = "annual"
	// BasisMonthly tests the rent for a single month.
	BasisMonthly Basis = "monthly"
	// BasisNone is section 195: there is no threshold and the first rupee is
	// deductible. Named rather than left as a zero threshold, because a zero
	// threshold reads like an unfilled field and this one is the statute.
	BasisNone Basis = "none"
)

// Timing is when the deduction falls due.
//
// It is a field rather than a note because it drives the schedule: section 194-I
// deducts every time rent is paid or credited, and section 194-IB deducts once, at
// the end of the year or when the tenancy ends, on the whole year's rent at once.
// A product that bills the second one monthly produces eleven months of wrong
// challans and one large surprise.
type Timing string

const (
	// EachPayment deducts on payment or credit, whichever is earlier.
	EachPayment Timing = "each_payment"
	// OnceAtYearOrLeaseEnd deducts once, in the last month of the financial year or
	// the last month of the tenancy, whichever comes first.
	OnceAtYearOrLeaseEnd Timing = "once_at_year_or_lease_end"
)

// Artefact is a document or reference the path obliges somebody to produce.
//
// Held as a list per path rather than as prose, because the tracking screen, the
// reminder schedule and the owner's statement all need the same list and none of
// them can read a paragraph.
type Artefact string

const (
	// Challan is the bank challan depositing the deduction — ITNS 281.
	Challan Artefact = "challan_281"
	// BookEntry24G is a government deductor's book adjustment in place of a challan.
	BookEntry24G Artefact = "book_entry_24g"
	// Return26Q is the quarterly return for deductions from residents.
	Return26Q Artefact = "return_26q"
	// Return27Q is the quarterly return for deductions from non-residents. Not 26Q,
	// and filing the wrong one is a defective return rather than a typo.
	Return27Q Artefact = "return_27q"
	// Form26QC is the challan-cum-statement that is section 194-IB's whole filing:
	// no TAN, no quarterly return, one form.
	Form26QC Artefact = "form_26qc"
	// Form16C is the certificate a section 194-IB deductor gives the landlord.
	Form16C Artefact = "form_16c"
	// Form16A is the quarterly certificate for every other path.
	Form16A Artefact = "form_16a"
	// Form15CA is the remitter's declaration for a payment to a non-resident.
	Form15CA Artefact = "form_15ca"
	// Form15CB is the chartered accountant's certificate that accompanies it above
	// the prescribed limits.
	Form15CB Artefact = "form_15cb"
)

// Path is what a pair of facts selects: a section, and everything that follows
// from it.
//
// Every field is a consequence of Section. They are spelled out rather than
// recomputed by each caller, because the failure mode of "the notifications team
// works out 194-IB's certificate for themselves" is a reminder for a Form 16A that
// nobody will ever issue.
type Path struct {
	Section Section

	// RateKey and ThresholdKey are the registry coordinates Assess resolves. Held on
	// the path so a gap can be named before anybody asks for a number.
	RateKey      string
	ThresholdKey string

	Basis  Basis
	Timing Timing

	// RequiresTAN is whether the deductor needs a tax deduction account number.
	// False for exactly one path, and that is the reason section 194-IB exists: an
	// individual tenant should not have to obtain a TAN to rent a flat.
	RequiresTAN bool

	// Artefacts is what this path obliges, in the order it is produced.
	Artefacts []Artefact

	// RequiresAcknowledgement is whether the lease may not be completed until the
	// deductor has been shown the obligation and accepted it. True for section 195
	// only: it is the path where an unaware tenant is most exposed, because the
	// deduction runs from the first rupee and the liability for missing it is theirs.
	RequiresAcknowledgement bool

	// Note is the one sentence the UI shows beside the section.
	Note string
}

// paths is the matrix's right-hand side. One entry per section, and the
// left-hand side — which pair of facts selects which — is Select below.
var paths = map[Section]Path{
	Section194I: {
		Section:      Section194I,
		RateKey:      "tds.194i_land_and_building",
		ThresholdKey: "tds.194i_annual",
		Basis:        BasisAnnual,
		Timing:       EachPayment,
		RequiresTAN:  true,
		Artefacts:    []Artefact{Challan, Return26Q, Form16A},
		Note: "Deducted on every payment or credit once the year's rent to this landlord " +
			"exceeds the threshold, and deposited by the seventh of the following month.",
	},
	Section194IB: {
		Section:      Section194IB,
		RateKey:      "tds.194ib_individual_huf",
		ThresholdKey: "tds.194ib_monthly",
		Basis:        BasisMonthly,
		Timing:       OnceAtYearOrLeaseEnd,
		RequiresTAN:  false,
		Artefacts:    []Artefact{Form26QC, Form16C},
		Note: "Deducted once, in the last month of the financial year or of the tenancy, " +
			"on the whole period's rent. No TAN and no quarterly return — Form 26QC is the filing.",
	},
	Section195: {
		Section: Section195,
		RateKey: "tds.195_non_resident",
		// No threshold key on purpose: there is no threshold to resolve, and an empty
		// coordinate is refused by Assess rather than resolved to something.
		ThresholdKey:            "",
		Basis:                   BasisNone,
		Timing:                  EachPayment,
		RequiresTAN:             true,
		Artefacts:               []Artefact{Challan, Return27Q, Form16A, Form15CA, Form15CB},
		RequiresAcknowledgement: true,
		Note: "The landlord is a non-resident, so tax is deducted from the first rupee — " +
			"there is no small-value exemption — and the tenant carries the liability for " +
			"failing to deduct. The rate depends on the Act and on any treaty, and is not " +
			"a number this product will guess.",
	},
}

// PathFor returns the fixed consequences of a section.
func PathFor(s Section) (Path, bool) {
	p, ok := paths[s]
	return p, ok
}

// Facts are the two answers, and when they were given.
//
// From is when they took effect, not when they were typed: a landlord who becomes
// non-resident in October has been a resident since April, and both facts are true
// of the same lease in the same year. That is why this is a value on a timeline
// (see History) rather than two columns that get overwritten.
type Facts struct {
	Deductor  DeductorClass
	Residency Residency

	// From is the date this pair became true. Required — a fact with no date cannot
	// be applied to a period.
	From effective.Date

	// Source is who said so: the tenant's own declaration, the landlord's, or a
	// document. Recorded because residency is asserted rather than proved, and when
	// the assessing officer asks, "the tenant declared it on this date" is an answer
	// and "the system had it" is not.
	Source string

	// AcknowledgedOn and AcknowledgedBy record that the deductor was shown the
	// obligation and accepted it. Required only where the path requires it.
	AcknowledgedOn effective.Date
	AcknowledgedBy string
}

// ErrFacts is a malformed or incomplete pair of facts.
var ErrFacts = errors.New("tds: the lease does not say enough to choose a section")

// ErrNotAcknowledged is a section 195 lease whose deductor has not accepted the
// obligation. Distinguishable, because the caller's response is to show a screen
// rather than to fix data.
var ErrNotAcknowledged = errors.New("tds: the deductor has not acknowledged the section 195 obligation")

// Validate refuses facts that cannot select a path.
func (f Facts) Validate() error {
	switch {
	case !f.Deductor.Valid():
		return fmt.Errorf("%w: %q is not a deductor class — an individual or HUF below audit, "+
			"one above it, a business, or a government body", ErrFacts, f.Deductor)
	case !f.Residency.Valid():
		return fmt.Errorf("%w: %q is not a residency", ErrFacts, f.Residency)
	case f.From.Zero():
		return fmt.Errorf("%w: the facts do not say when they became true, so no period can be "+
			"assessed with them", ErrFacts)
	case !f.AcknowledgedOn.Zero() && f.AcknowledgedBy == "":
		return fmt.Errorf("%w: an acknowledgement dated %s that names nobody is not an "+
			"acknowledgement", ErrFacts, f.AcknowledgedOn)
	}
	return nil
}

// Select is the matrix.
//
// Residency is tested first and it is not a tie-break: a non-resident landlord puts
// every deductor on section 195, including the salaried tenant who would otherwise
// have deducted under 194-IB and the company that would have deducted under 194-I.
// The sections are mutually exclusive in that order, and reading them the other way
// round — deciding on the deductor and then adjusting for residency — is the
// mistake that produces a 194-IB deduction at two per cent on a payment abroad.
func Select(f Facts) (Path, error) {
	if err := f.Validate(); err != nil {
		return Path{}, err
	}
	switch {
	case f.Residency == NonResident:
		return paths[Section195].forDeductor(f.Deductor), nil
	case f.Deductor == IndividualNoAudit:
		return paths[Section194IB].forDeductor(f.Deductor), nil
	default:
		return paths[Section194I].forDeductor(f.Deductor), nil
	}
}

// forDeductor makes the copy the caller gets, with the one artefact that depends
// on the deductor rather than the section: a government deductor adjusts the
// deduction by book entry and reports it on Form 24G, and never holds a challan
// number. Everything else about the path is the section's.
func (p Path) forDeductor(d DeductorClass) Path {
	out := p
	out.Artefacts = slices.Clone(p.Artefacts)
	if d != Government {
		return out
	}
	for i, a := range out.Artefacts {
		if a == Challan {
			out.Artefacts[i] = BookEntry24G
		}
	}
	return out
}

// Acknowledged reports whether the facts carry an acknowledgement that covers this
// path.
//
// A path that needs none is acknowledged trivially. The acknowledgement belongs to
// the facts it is stored with, so this asks only that it exists and names somebody;
// what stops a stale one being carried forward onto a later set of facts is
// NewHistory, which is the only place that can see that a set of facts supersedes
// another.
func (p Path) Acknowledged(f Facts) bool {
	if !p.RequiresAcknowledgement {
		return true
	}
	return !f.AcknowledgedOn.Zero() && f.AcknowledgedBy != ""
}

// RequireAcknowledgement is the lease-completion gate, as an error a handler can
// return unchanged.
func (p Path) RequireAcknowledgement(f Facts) error {
	if p.Acknowledged(f) {
		return nil
	}
	return fmt.Errorf("%w: tax is deducted from the first rupee and the deductor carries the "+
		"liability for failing to deduct — the lease cannot be completed until that is accepted",
		ErrNotAcknowledged)
}

// History is the facts over time: one interval per pair, contiguous, no overlaps.
//
// Modelled as a timeline for the reason ADR-0008 gives generally and for one
// specific reason here: a residency that changes mid-lease has to leave the earlier
// months assessed as they actually were. Recomputing April under October's facts
// would restate a deduction that has already been deposited and certified.
type History struct {
	Timeline effective.Timeline[Facts]
}

// NewHistory validates each entry and the shape of the whole.
//
// It is also where a carried-forward acknowledgement is caught. The first set of
// facts may be acknowledged before the tenancy starts — that is what signing is —
// but a set that supersedes another may not be acknowledged before it arose: a
// tenant who accepted a section 195 obligation in April did not thereby accept the
// one that appeared when the landlord left the country in October, and copying the
// old date onto the new row is exactly how that would be claimed.
func NewHistory(records []effective.Record[Facts]) (History, error) {
	h := History{Timeline: effective.Timeline[Facts]{Records: records}}
	for i, r := range records {
		if err := r.Value.Validate(); err != nil {
			return History{}, err
		}
		if !r.Range.Valid() {
			return History{}, fmt.Errorf("%w: a fact with no validity interval", ErrFacts)
		}
		if !r.Range.From().Equal(r.Value.From) {
			return History{}, fmt.Errorf("%w: facts effective %s stored in an interval starting %s",
				ErrFacts, r.Value.From, r.Range.From())
		}
		if i > 0 && !r.Value.AcknowledgedOn.Zero() && r.Value.AcknowledgedOn.Before(r.Value.From) {
			return History{}, fmt.Errorf("%w: facts effective %s carry an acknowledgement dated %s, "+
				"which was given against the facts they replace", ErrFacts, r.Value.From, r.Value.AcknowledgedOn)
		}
	}
	if err := h.Timeline.Validate(); err != nil {
		return History{}, err
	}
	return h, nil
}

// AsOf returns the facts in force on a date.
func (h History) AsOf(on effective.Date) (Facts, bool) {
	rec, ok := h.Timeline.AsOf(on)
	if !ok {
		return Facts{}, false
	}
	return rec.Value, true
}

// PathOn selects the section that applied on a date.
func (h History) PathOn(on effective.Date) (Path, Facts, error) {
	f, ok := h.AsOf(on)
	if !ok {
		return Path{}, Facts{}, fmt.Errorf("%w: nothing says what the deductor and the landlord "+
			"were on %s", ErrFacts, on)
	}
	p, err := Select(f)
	return p, f, err
}

// Changes reports every date on which the selected section changes, in order.
//
// It exists so the lease screen can say "this tenancy is on two sections this year"
// before the payout run discovers it, and so the tests for the residency-change
// edge case assert on something other than two separate lookups.
func (h History) Changes() ([]effective.Date, error) {
	live := h.Timeline.Live()
	var (
		out  []effective.Date
		last Section
	)
	for i, rec := range live {
		p, err := Select(rec.Value)
		if err != nil {
			return nil, err
		}
		if i > 0 && p.Section != last {
			out = append(out, rec.Range.From())
		}
		last = p.Section
	}
	return out, nil
}

// SectionOf is the statutory citation for a section, for a challan reference and
// for anything a human reads.
func (s Section) String() string {
	switch s {
	case Section194I:
		return "194-I"
	case Section194IB:
		return "194-IB"
	case Section195:
		return "195"
	}
	return string(s)
}

// Statute is the full citation.
func (s Section) Statute() string {
	switch s {
	case Section194I:
		return "Section 194-I(b), Income-tax Act 1961"
	case Section194IB:
		return "Section 194-IB, Income-tax Act 1961"
	case Section195:
		return "Section 195, Income-tax Act 1961"
	}
	return ""
}

// ruleTypes ties the matrix back to the registry's vocabulary, so a rename in
// statutory that this package did not follow fails a test rather than a payout.
var ruleTypes = struct{ Rate, Threshold statutory.Type }{
	Rate:      statutory.TDSRate,
	Threshold: statutory.TDSThreshold,
}
