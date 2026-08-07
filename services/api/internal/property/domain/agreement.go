package domain

import (
	"regexp"
)

// The owner–manager agreement (#340), India. The clauses are the platform's
// own wording of the stored template: a manager may enter and manage, may not
// sell or deal, and an owner intending to sell gives four months' notice —
// long enough to place the tenancies the manager has already let.

// Clause is one numbered term.
type Clause struct {
	Number  string
	Heading string
	Text    string
}

// Signature is a place to sign. Nothing is signed online yet, so what the PDF
// carries is the block and the paper carries the signature.
type Signature struct {
	Role  string
	Name  string
	Lines []string
}

// Agreement is the whole instrument, ready to print.
type Agreement struct {
	Title      string
	Recitals   []string
	Clauses    []Clause
	Signatures []Signature
}

// SaleNoticeMonths is the deviation from the template's "inform immediately":
// a sale ends tenancies the manager placed, so the notice is a term.
const SaleNoticeMonths = 4

var placeholder = regexp.MustCompile(`\{\{([a-z0-9_]+)\}\}`)

// AgreementFields is every merge field this instrument fills, sorted. The
// platform template declares the same set (#341), so a firm's revision that
// drops one is refused before it can print a blank clause.
func AgreementFields() []string {
	fields, _ := InstrumentFields("management_agreement")
	return fields
}

// BuildAgreement fills the instrument. A field left unfilled is refused rather
// than printed: a clause reading "{{management_fee_pct}}" is a figure somebody
// signs believing it is there.
func BuildAgreement(fields map[string]string) (Agreement, error) {
	return BuildInstrument("management_agreement", fields)
}

var recitals = []string{
	"This agreement is made at {{execution_place}} on {{execution_date}}.",
	"BETWEEN {{owner_name}}, residing at {{owner_address}}, PAN {{owner_pan}} (the Owner),",
	"AND {{manager_name}}, of {{manager_address}}, RERA registration {{manager_rera}} (the Property Manager).",
	"WHEREAS the Owner is the lawful owner of {{property_address}} ({{property_description}}) (the Property), " +
		"and wishes to appoint the Property Manager to let and manage it.",
}

var clauses = []Clause{
	{"1", "Appointment", "The Owner appoints the Property Manager to let, manage and maintain the Property " +
		"for a term of {{term_months}} months commencing {{commencement_date}}."},
	{"2", "Fee", "The Property Manager shall be paid {{management_fee_pct}}% of the rent collected, " +
		"deducted from collections before the balance is released to the Owner."},
	{"3", "Access", "The Property Manager and its authorised staff may enter the Property to show it to " +
		"prospective tenants, to inspect it, and to carry out repairs, on reasonable notice to any occupant."},
	{"4.1", "Authority", "The Property Manager may advertise the Property, screen and select tenants, sign " +
		"tenancy agreements on the Owner's behalf, collect rent and deposits, and instruct repairs within the " +
		"limit in clause 5."},
	{"4.2", "No authority to sell or deal", "The Property Manager has no authority to sell, mortgage, gift, " +
		"charge, encumber or otherwise transact in the Property or any interest in it, and shall not hold itself " +
		"out as having such authority. No transaction of that kind is within this appointment. This clause " +
		"survives termination."},
	{"5", "Repairs", "The Property Manager may instruct repairs up to Rs. {{repair_ceiling}} per item without " +
		"prior approval, and shall obtain the Owner's approval above that figure. Urgent repairs needed to keep " +
		"the Property safe or habitable may be instructed at once and reported the same day."},
	{"6", "Owner's responsibilities", "The Owner shall keep the Property structurally sound, insured, and free " +
		"of encumbrance; shall clear property tax, society dues and utility arrears; and shall meet the cost of " +
		"structural repair, seepage and any statutory upgrade."},
	{"7", "Manager's responsibilities", "The Property Manager shall account for every sum collected, hold " +
		"deposits as the tenancy requires, deduct and deposit tax where the law requires it, report monthly, " +
		"and keep the Owner informed of anything affecting the Property's letting."},
	{"8.1", "Documents", "The Owner shall furnish the title deed, or the power of attorney under which " +
		"another person acts, before the Property is marketed."},
	{"8.3", "Fit for occupation", "The Owner warrants the Property is fit for occupation, with safe electrical " +
		"wiring, working sanitation and water supply, and shall permit the inspections in the onboarding " +
		"checklist before a tenancy begins."},
	{"9", "Intention to sell", "If the Owner intends to sell or otherwise dispose of the Property, the Owner " +
		"shall give the Property Manager not less than four (4) months' written notice, so that the tenancies " +
		"already placed can be managed to their end and any occupant given lawful notice."},
	{"10", "Termination", "Either party may terminate on ninety (90) days' written notice. Termination does " +
		"not end a tenancy already granted, and the Property Manager shall hand over agreements, deposits, keys " +
		"and records within thirty (30) days."},
	{"11", "Governing law", "This agreement is governed by the laws of India, and the courts at " +
		"{{execution_place}} have jurisdiction."},
}

var signatures = []Signature{
	{Role: "Owner", Name: "{{owner_name}}", Lines: []string{"Signature", "Date", "Place"}},
	{Role: "Property Manager", Name: "{{manager_name}}",
		Lines: []string{"Signature", "Name and designation", "Date", "Place"}},
	{Role: "Witness 1", Name: "", Lines: []string{"Signature", "Name", "Address"}},
	{Role: "Witness 2", Name: "", Lines: []string{"Signature", "Name", "Address"}},
}
