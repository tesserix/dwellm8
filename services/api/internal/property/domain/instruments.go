package domain

import (
	"fmt"
	"sort"
	"strings"
)

// The instruments a firm issues (#341), as the platform's own India wording.
// A preview prints the blank form so a manager can read what they are about to
// issue before an owner or a tenant is asked to sign it (#350).

// Instrument is one printable legal document.
type Instrument struct {
	Kind       string
	Title      string
	Recitals   []string
	Clauses    []Clause
	Signatures []Signature
}

// blank is what a merge field prints as on an unfilled form: a ruled space for
// a pen, never the placeholder itself.
const blank = "____________________"

var instruments = map[string]Instrument{
	"management_agreement": {
		Kind: "management_agreement", Title: "PROPERTY MANAGEMENT AGREEMENT",
		Recitals: recitals, Clauses: clauses, Signatures: signatures,
	},
	"rent_agreement":       rentAgreement,
	"lease_deed":           leaseDeed,
	"power_of_attorney":    powerOfAttorney,
	"onboarding_checklist": onboardingChecklist,
}

// InstrumentFor reads one by kind.
func InstrumentFor(kind string) (Instrument, error) {
	in, ok := instruments[strings.TrimSpace(kind)]
	if !ok {
		return Instrument{}, fmt.Errorf("this firm does not issue a %q", kind)
	}
	return in, nil
}

// InstrumentFields is every merge field the instrument fills, sorted.
func InstrumentFields(kind string) ([]string, error) {
	in, err := InstrumentFor(kind)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range in.sources() {
		for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
			seen[m[1]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

// BuildInstrument fills the form. A field left unfilled is refused rather than
// printed: a clause reading "{{rent_amount}}" is a figure somebody signs
// believing it is there.
func BuildInstrument(kind string, fields map[string]string) (Agreement, error) {
	in, err := InstrumentFor(kind)
	if err != nil {
		return Agreement{}, err
	}
	declared, err := InstrumentFields(kind)
	if err != nil {
		return Agreement{}, err
	}
	var missing []string
	for _, f := range declared {
		if strings.TrimSpace(fields[f]) == "" {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return Agreement{}, fmt.Errorf("the agreement cannot print without %s", strings.Join(missing, ", "))
	}
	return in.render(func(f string) string { return strings.TrimSpace(fields[f]) }), nil
}

// PreviewInstrument prints the blank form, every field ruled for a pen.
func PreviewInstrument(kind string) (Agreement, error) {
	in, err := InstrumentFor(kind)
	if err != nil {
		return Agreement{}, err
	}
	return in.render(func(string) string { return blank }), nil
}

func (in Instrument) render(value func(field string) string) Agreement {
	fill := func(s string) string {
		return placeholder.ReplaceAllStringFunc(s, func(m string) string {
			return value(placeholder.FindStringSubmatch(m)[1])
		})
	}
	a := Agreement{Title: in.Title}
	for _, r := range in.Recitals {
		a.Recitals = append(a.Recitals, fill(r))
	}
	for _, c := range in.Clauses {
		a.Clauses = append(a.Clauses, Clause{Number: c.Number, Heading: c.Heading, Text: fill(c.Text)})
	}
	for _, s := range in.Signatures {
		a.Signatures = append(a.Signatures, Signature{Role: s.Role, Name: fill(s.Name), Lines: s.Lines})
	}
	return a
}

func (in Instrument) sources() []string {
	out := append([]string{}, in.Recitals...)
	for _, c := range in.Clauses {
		out = append(out, c.Text)
	}
	for _, s := range in.Signatures {
		out = append(out, s.Name)
	}
	return out
}

// Eleven months, because twelve or more attracts compulsory registration under
// the Registration Act and the stamp duty of a lease.
var rentAgreement = Instrument{
	Kind: "rent_agreement", Title: "RENT AGREEMENT (ELEVEN MONTHS)",
	Recitals: []string{
		"This agreement is made at {{execution_place}} on {{execution_date}}.",
		"BETWEEN {{owner_name}}, residing at {{owner_address}}, PAN {{owner_pan}} (the Landlord),",
		"AND {{tenant_name}}, residing at {{tenant_address}} (the Tenant).",
		"WHEREAS the Landlord is the lawful owner of {{property_address}} ({{property_description}}) " +
			"(the Premises) and has agreed to let it to the Tenant on the terms below.",
	},
	Clauses: []Clause{
		{"1", "Term", "The Landlord lets the Premises to the Tenant for eleven (11) months commencing " +
			"{{commencement_date}}. The term is eleven months so that the agreement is not a lease requiring " +
			"compulsory registration, and it may be renewed by a fresh agreement."},
		{"2", "Rent", "The Tenant shall pay Rs. {{rent_amount}} per month, in advance, on or before the " +
			"{{rent_due_day}} day of each month, by bank transfer to the account the Landlord notifies."},
		{"3", "Deposit", "The Tenant has paid an interest-free security deposit of Rs. {{deposit_amount}}, " +
			"refundable within thirty (30) days of vacant possession, less lawful deductions for unpaid rent, " +
			"unpaid utilities and damage beyond fair wear and tear, each itemised in writing."},
		{"4", "Utilities and outgoings", "The Tenant shall pay electricity, water, gas, internet and any " +
			"metered charge for the period of occupation. Property tax, society corpus and structural charges " +
			"remain the Landlord's."},
		{"5", "Use", "The Premises shall be used for residence only, by the Tenant and the occupants declared " +
			"to the Landlord. It shall not be sublet, assigned or used for any trade or unlawful purpose."},
		{"6", "Maintenance", "The Tenant shall keep the Premises in the condition received, fair wear and tear " +
			"excepted, and shall meet the cost of minor repairs. The Landlord shall meet structural repair, " +
			"seepage, and the repair or replacement of fittings that fail through age."},
		{"7", "Entry", "The Landlord, or the property manager appointed by the Landlord, may enter the Premises " +
			"on twenty-four (24) hours' notice to inspect, to carry out repairs, or to show it to a prospective " +
			"tenant in the last month of the term. Entry without notice is permitted only in an emergency."},
		{"8", "Notice", "Either party may end this agreement on one (1) month's written notice, or by paying " +
			"one month's rent in lieu. Notice given by the Tenant does not release rent already due."},
		{"9", "Inventory and condition", "The inventory and condition report signed at handover is part of this " +
			"agreement, and the deposit is settled against it at move-out."},
		{"10", "Tax deduction at source", "Where the law requires the Tenant to deduct tax at source on rent, " +
			"the Tenant shall deduct it, deposit it against the Landlord's PAN, and furnish the certificate."},
		{"11", "Governing law", "This agreement is governed by the laws of India, and the courts at " +
			"{{execution_place}} have jurisdiction."},
	},
	Signatures: []Signature{
		{Role: "Landlord", Name: "{{owner_name}}", Lines: []string{"Signature", "Date", "Place"}},
		{Role: "Tenant", Name: "{{tenant_name}}", Lines: []string{"Signature", "Date", "Place"}},
		{Role: "Witness 1", Name: "", Lines: []string{"Signature", "Name", "Address"}},
		{Role: "Witness 2", Name: "", Lines: []string{"Signature", "Name", "Address"}},
	},
}

// Twelve months or more: this one is registered, and says so.
var leaseDeed = Instrument{
	Kind: "lease_deed", Title: "DEED OF LEASE",
	Recitals: []string{
		"This deed of lease is made at {{execution_place}} on {{execution_date}}.",
		"BETWEEN {{owner_name}}, residing at {{owner_address}}, PAN {{owner_pan}} (the Lessor),",
		"AND {{tenant_name}}, residing at {{tenant_address}} (the Lessee).",
		"WHEREAS the Lessor is the absolute owner of {{property_address}} ({{property_description}}) " +
			"(the Demised Premises) and has agreed to demise it to the Lessee for the term below.",
	},
	Clauses: []Clause{
		{"1", "Demise and term", "The Lessor demises the Demised Premises to the Lessee for {{term_months}} " +
			"months commencing {{commencement_date}}. As the term is twelve months or more, this deed shall be " +
			"stamped and registered under the Registration Act, 1908, before possession is acted upon."},
		{"2", "Rent and escalation", "The Lessee shall pay Rs. {{rent_amount}} per month in advance by the " +
			"{{rent_due_day}} day of each month, escalating by {{escalation_pct}}% on each anniversary of the " +
			"commencement date."},
		{"3", "Deposit", "The Lessee has paid an interest-free security deposit of Rs. {{deposit_amount}}, " +
			"refundable within thirty (30) days of vacant possession less lawful, itemised deductions."},
		{"4", "Lock-in", "Neither party may terminate within the first {{lock_in_months}} months. A Lessee " +
			"vacating within the lock-in remains liable for the rent of the unexpired period."},
		{"5", "Registration and stamp duty", "The stamp duty and registration fee on this deed shall be borne " +
			"{{duty_borne_by}}. The parties shall attend the office of the Sub-Registrar to register it."},
		{"6", "Quiet enjoyment", "The Lessee paying the rent and observing these covenants shall peaceably hold " +
			"and enjoy the Demised Premises without interruption by the Lessor or anyone claiming under it."},
		{"7", "Repairs", "The Lessee shall keep the interior in good repair, fair wear and tear excepted. The " +
			"Lessor shall carry out structural repair and repair to the roof, walls, drains and common services."},
		{"8", "Alterations", "The Lessee shall make no structural alteration without the Lessor's written " +
			"consent, and shall restore any permitted alteration at the end of the term if required."},
		{"9", "Entry", "The Lessor or its property manager may enter on twenty-four (24) hours' notice to " +
			"inspect or repair, and without notice in an emergency."},
		{"10", "Determination", "On expiry or earlier determination the Lessee shall deliver vacant possession " +
			"with all keys, fixtures and fittings, and the deposit shall be settled against the inventory."},
		{"11", "Tax deduction at source", "The Lessee shall deduct tax at source on the rent where the law " +
			"requires it, deposit it against the Lessor's PAN, and furnish the certificate."},
		{"12", "Governing law", "This deed is governed by the laws of India, and the courts at " +
			"{{execution_place}} have jurisdiction."},
	},
	Signatures: []Signature{
		{Role: "Lessor", Name: "{{owner_name}}", Lines: []string{"Signature", "Date", "Place"}},
		{Role: "Lessee", Name: "{{tenant_name}}", Lines: []string{"Signature", "Date", "Place"}},
		{Role: "Witness 1", Name: "", Lines: []string{"Signature", "Name", "Address"}},
		{Role: "Witness 2", Name: "", Lines: []string{"Signature", "Name", "Address"}},
	},
}

// Limited, and it says what it excludes: a power of attorney silent on sale is
// read as a general one.
var powerOfAttorney = Instrument{
	Kind: "power_of_attorney", Title: "LIMITED POWER OF ATTORNEY",
	Recitals: []string{
		"This power of attorney is executed at {{execution_place}} on {{execution_date}}.",
		"I, {{owner_name}}, residing at {{owner_address}}, PAN {{owner_pan}} (the Principal), being the " +
			"lawful owner of {{property_address}} ({{property_description}}) (the Property),",
		"DO HEREBY APPOINT {{manager_name}}, of {{manager_address}} (the Attorney), to act for me in the " +
			"limited matters set out below and in no other matter whatsoever.",
	},
	Clauses: []Clause{
		{"1", "Powers granted", "The Attorney may advertise and show the Property, negotiate and sign tenancy " +
			"agreements on my behalf for a term not exceeding {{max_term_months}} months, collect rent and " +
			"deposits, issue receipts, and instruct repairs within the limit I have agreed in writing."},
		{"2", "Statutory and utility matters", "The Attorney may correspond with the society, the municipal " +
			"authority and the utility providers on my behalf, and may pay outgoings from the rent collected."},
		{"3", "No power to sell or deal", "The Attorney has no power to sell, mortgage, gift, exchange, charge, " +
			"encumber, surrender or otherwise transfer the Property or any interest in it, no power to create " +
			"any tenancy exceeding the term in clause 1, and no power to sign any instrument requiring " +
			"compulsory registration other than one I have separately authorised in writing."},
		{"4", "Money", "Rent and deposits collected are held for me and shall be accounted for monthly and " +
			"remitted to my account {{owner_account}}, less only the sums I have agreed in writing."},
		{"5", "Duration and revocation", "This power takes effect on {{commencement_date}} and continues until " +
			"{{expiry_date}} unless I revoke it earlier. I may revoke it at any time by written notice, and the " +
			"Attorney shall then hand over agreements, deposits, keys and records within thirty (30) days. " +
			"Anything lawfully done before revocation remains binding on me."},
		{"6", "No delegation", "The Attorney shall not delegate any power granted here to another person " +
			"without my written consent."},
		{"7", "Governing law", "This power of attorney is governed by the laws of India, and the courts at " +
			"{{execution_place}} have jurisdiction."},
	},
	Signatures: []Signature{
		{Role: "Principal", Name: "{{owner_name}}", Lines: []string{"Signature", "Date", "Place"}},
		{Role: "Attorney (accepting the appointment)", Name: "{{manager_name}}",
			Lines: []string{"Signature", "Name and designation", "Date"}},
		{Role: "Witness 1", Name: "", Lines: []string{"Signature", "Name", "Address"}},
		{Role: "Witness 2", Name: "", Lines: []string{"Signature", "Name", "Address"}},
	},
}

// What has to be true before the keys change hands. It is signed because a
// dispute at move-out is settled against what both sides agreed at handover.
var onboardingChecklist = Instrument{
	Kind: "onboarding_checklist", Title: "OWNER AND MANAGER ONBOARDING CHECKLIST",
	Recitals: []string{
		"Property: {{property_address}} ({{property_description}}).",
		"Owner: {{owner_name}}.    Manager: {{manager_name}}.    Date of handover: {{execution_date}}.",
		"Each item below is inspected together and marked pass or fail. A failed item is listed with the " +
			"date it will be put right, and the property is not marketed until every item passes.",
	},
	Clauses: []Clause{
		{"1", "Title and authority", "Title deed or allotment letter sighted [ ]    Power of attorney, where " +
			"another person acts [ ]    Owner's PAN and bank account recorded [ ]    Society no-objection, " +
			"where the society requires one [ ]"},
		{"2", "Electrical", "Wiring, earthing and the distribution board inspected [ ]    Residual current " +
			"device present and tested [ ]    No exposed conductor or temporary joint [ ]    Electricity bill " +
			"cleared to date [ ]"},
		{"3", "Water and sanitation", "Supply running at adequate pressure [ ]    Tank cleaned and covered [ ]  " +
			"  Every tap, trap and flush working [ ]    No leak or seepage on the ceiling or wall [ ]"},
		{"4", "Fire and safety", "Escape route clear [ ]    Extinguisher present and within its service date [ ]" +
			"    Smoke alarm where the building requires one [ ]    Balcony rail and grille secure [ ]"},
		{"5", "Pest and damp", "Termite inspection carried out [ ]    Treatment done and warranty recorded [ ]  " +
			"  No damp patch, mould or rising damp [ ]"},
		{"6", "Fittings and inventory", "Every fixture, fitting and appliance listed with its condition [ ]    " +
			"Meter readings recorded [ ]    Keys counted and handed over [ ]    Photographs taken and filed [ ]"},
		{"7", "Statutory", "Property tax paid to date [ ]    Society dues cleared [ ]    Occupancy certificate " +
			"on file [ ]    Where the property is a hostel or PG, the trade licence, police verification and " +
			"fire clearance are current [ ]"},
		{"8", "Declaration", "The owner declares the property fit for occupation and permits the inspections " +
			"above to be repeated at the end of each tenancy. Items failed on the date of handover are listed " +
			"here: {{outstanding_items}}"},
	},
	Signatures: []Signature{
		{Role: "Owner", Name: "{{owner_name}}", Lines: []string{"Signature", "Date"}},
		{Role: "Manager", Name: "{{manager_name}}", Lines: []string{"Signature", "Name and designation", "Date"}},
	},
}
