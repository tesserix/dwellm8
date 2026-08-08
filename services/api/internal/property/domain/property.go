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
	UnitCount int
	// What the building is like (#354).
	About     string
	Amenities []string
}

// PropertyDetail is what a manager writes about the building, as opposed to
// where it is. Separate from Property because it is written on its own: the
// address is set at onboarding and the description months later.
type PropertyDetail struct {
	About     string
	Amenities []string
}

// Unit is one lettable space in a property, as the register holds it. What is
// let, to whom and for how much is the lease module's answer, not this one's;
// the bedroom count still lives on the advert (#338).
type Unit struct {
	ID         string
	PropertyID string
	Code       string
	Kind       string
	Floor      int
	Occupancy  string
	CarpetSqf  float64
	// Set on a unit read one at a time; the list read leaves them empty.
	BuiltupSqf         float64
	ShareCertificateNo string
	ElectricityNo      string
	WaterNo            string
	UnitDetail
}

// UnitDetail is what the flat is like (#354). The counts are pointers because
// "no bathroom recorded" and "no bathroom" are different answers, and only one
// of them should be shown to a renter.
type UnitDetail struct {
	About          string
	Features       []string
	Bathrooms      *int
	Balconies      *int
	CoveredParking *int
	Facing         string
	Furnishing     string
}

// Place is somewhere near a property, and how far it is. Distance is in metres
// and is what the manager measured, not a straight line between two geocodes.
type Place struct {
	ID         string
	Category   string
	Name       string
	DistanceM  int
	TravelMode string
	Tags       []string
	Note       string
	Position   int
}

// Bed is what is let in a hostel or a PG (#299): a bed in a room, priced on
// its own. Sharing is how many beds the room holds.
type Bed struct {
	ID        string
	Label     string
	UnitID    string
	Room      string
	Floor     int
	Sharing   int
	RentMinor int64
	State     string
	LeaseID   string
}
