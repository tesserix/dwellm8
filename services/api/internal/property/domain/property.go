// Package domain holds the property tree's shape: property, block, unit —
// ADR-0009. No framework imports, per the module contract in property/doc.go.
package domain

// Property is one entry in an owner's portfolio, as read back — this module
// is a reader in Phase 1 (#see USER_REQUIREMENT.md), not yet a writer.
type Property struct {
	ID           string
	Code         string
	Name         string
	Kind         string
	AddressLine1 string
	AddressLine2 string
	Locality     string
	City         string
	StateCode    string
	Pin          string
	// UnitCount is how many lettable units (not ancillaries) the property has.
	// Bedroom/bathroom counts have no column in this schema (020_property_
	// block_unit.sql models rooms as units, not a count on the flat) — a
	// caller wanting them has to derive them from Units, not read them here.
	UnitCount int
}

// Unit is one lettable space in a property, as the register holds it. What is
// let, to whom and for how much is the lease module's answer, not this one's.
type Unit struct {
	ID        string
	Code      string
	Kind      string
	Floor     int
	Occupancy string
	CarpetSqf float64
}
