package domain

import (
	"errors"
	"fmt"
	"regexp"
)

// Registering a property and its units — issue #32, the very start of the
// funnel. The schema (020) re-checks everything below with CHECKs and unique
// constraints; these rules exist so a refusal names the field, because
// "constraint violation" is not a thing anybody can fix from a phone.

// ErrDraft reports a draft that cannot become a property or unit.
var ErrDraft = errors.New("property: the draft is not complete enough to create")

// Kinds is the property vocabulary, the schema's own.
var Kinds = map[string]bool{
	"standalone": true, "building": true, "society": true,
	"commercial": true, "coliving": true, "plot": true,
}

// UnitKinds is the unit vocabulary, the schema's own.
var UnitKinds = map[string]bool{
	"flat": true, "floor": true, "room": true, "shop": true,
	"office": true, "desk": true, "parking": true, "storage": true,
}

// stateCodes is ISO 3166-2:IN, the same set the schema enforces.
var stateCodes = map[string]bool{
	"AN": true, "AP": true, "AR": true, "AS": true, "BR": true, "CH": true,
	"CT": true, "DH": true, "DL": true, "GA": true, "GJ": true, "HP": true,
	"HR": true, "JH": true, "JK": true, "KA": true, "KL": true, "LA": true,
	"LD": true, "MH": true, "ML": true, "MN": true, "MP": true, "MZ": true,
	"NL": true, "OR": true, "PB": true, "PY": true, "RJ": true, "SK": true,
	"TG": true, "TN": true, "TR": true, "UP": true, "UT": true, "WB": true,
}

// pinPattern: Indian PIN codes never start with zero.
var pinPattern = regexp.MustCompile(`^[1-9][0-9]{5}$`)

// PropertyDraft is what registering a building takes.
type PropertyDraft struct {
	Code         string
	Name         string
	Kind         string
	AddressLine1 string
	AddressLine2 string
	Locality     string
	City         string
	District     string
	StateCode    string
	Pin          string
}

// Validate refuses what cannot be a property, naming the field.
func (d PropertyDraft) Validate() error {
	switch {
	case d.Code == "":
		return fmt.Errorf("%w: code is required — the short name you will know it by", ErrDraft)
	case d.Name == "":
		return fmt.Errorf("%w: name is required", ErrDraft)
	case !Kinds[d.Kind]:
		return fmt.Errorf("%w: kind must be one of standalone, building, society, commercial, coliving, plot", ErrDraft)
	case d.AddressLine1 == "":
		return fmt.Errorf("%w: address_line1 is required", ErrDraft)
	case d.Locality == "" || d.City == "":
		return fmt.Errorf("%w: locality and city are required", ErrDraft)
	case !stateCodes[d.StateCode]:
		return fmt.Errorf("%w: state_code must be the two-letter state, e.g. KA", ErrDraft)
	case !pinPattern.MatchString(d.Pin):
		return fmt.Errorf("%w: pin must be a six-digit PIN code", ErrDraft)
	}
	return nil
}

// UnitDraft is what adding a lettable unit takes.
type UnitDraft struct {
	Code           string
	Kind           string
	Floor          *int
	CarpetAreaSqft float64
	// ParentUnitID attaches a parking slot or storage cage to its flat.
	ParentUnitID string
}

// Validate refuses what cannot be a unit.
func (d UnitDraft) Validate() error {
	ancillary := d.Kind == "parking" || d.Kind == "storage"
	switch {
	case d.Code == "":
		return fmt.Errorf("%w: code is required — '1204', 'P-31', 'Desk 7'", ErrDraft)
	case !UnitKinds[d.Kind]:
		return fmt.Errorf("%w: unit_kind must be one of flat, floor, room, shop, office, desk, parking, storage", ErrDraft)
	case !ancillary && d.CarpetAreaSqft <= 0:
		// The schema's units_lettable_has_area rule, said early: a flat with no
		// area cannot be priced, listed or stamped.
		return fmt.Errorf("%w: a lettable unit needs carpet_area_sqft", ErrDraft)
	case d.ParentUnitID != "" && !ancillary:
		// The schema's generated-column rule, said early: only an ancillary
		// unit parks on a parent.
		return fmt.Errorf("%w: only parking or storage attaches to a parent unit", ErrDraft)
	}
	return nil
}
