package domain

import "sort"

// What a firm may hold against a property (#339). The same vocabulary is a
// CHECK on documents.kind; it is repeated here so an unknown kind is a
// sentence with a field attached rather than a constraint violation.
var documentKinds = map[string]bool{
	"title_deed": true, "mother_deed": true, "tax_receipt": true,
	"encumbrance_certificate": true, "khata_patta": true,
	"occupancy_certificate": true, "society_noc": true, "lender_noc": true,
	"power_of_attorney": true, "management_agreement": true,
	"rent_agreement": true, "lease_deed": true, "inspection_report": true,
	"utility_bill": true, "insurance_policy": true, "floor_plan": true,
	"other": true,
}

// KnownDocumentKind reports whether a kind may be filed against a property.
func KnownDocumentKind(kind string) bool { return documentKinds[kind] }

// Either of these proves the owner may let: the deed, or the power of attorney
// under which somebody else acts for the owner (PMA 8.1).
var provesTheRightToLet = []string{"title_deed", "power_of_attorney"}

// Worth having and not what proves the right to let — an encumbrance
// certificate shows the property is unmortgaged, the rest place it on the
// municipal record. Missing one delays nothing.
var worthHolding = []string{
	"encumbrance_certificate", "tax_receipt", "khata_patta",
	"occupancy_certificate", "society_noc",
}

// Ownership is what a property's paperwork adds up to.
type Ownership struct {
	Proven bool
	// Held is every kind on file, once each.
	Held []string
	// Missing names what would prove the right to let, and is empty once one of
	// them is in. Advisory is what is still wanted but blocks nothing.
	Missing  []string
	Advisory []string
}

// OwnershipFrom reads the evidence off the kinds on file.
func OwnershipFrom(kinds []string) Ownership {
	on := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		if k != "" {
			on[k] = true
		}
	}

	out := Ownership{Held: make([]string, 0, len(on)), Missing: []string{}, Advisory: []string{}}
	for k := range on {
		out.Held = append(out.Held, k)
	}
	sort.Strings(out.Held)

	for _, k := range provesTheRightToLet {
		if on[k] {
			out.Proven = true
		}
	}
	if !out.Proven {
		out.Missing = append(out.Missing, provesTheRightToLet...)
	}
	for _, k := range worthHolding {
		if !on[k] {
			out.Advisory = append(out.Advisory, k)
		}
	}
	return out
}
