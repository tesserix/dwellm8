package ops

import (
	"errors"
	"net/http"

	discoveryservice "github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// WithListings gives the surface the advert on a flat — where the bedroom
// count lives, the register having none (#338).
func (h *Handler) WithListings(l *discoveryservice.Listings) *Handler {
	h.listings = l
	return h
}

// unitRecord is one flat: what the register holds, the building it is in, the
// tenancy in it, the advert that let it, and what is allotted to it.
type unitRecord struct {
	ID                    string  `json:"id"`
	Code                  string  `json:"code"`
	Kind                  string  `json:"kind"`
	Floor                 int     `json:"floor"`
	Occupancy             string  `json:"occupancy"`
	CarpetAreaSqft        float64 `json:"carpet_area_sqft,omitempty"`
	BuiltupAreaSqft       float64 `json:"builtup_area_sqft,omitempty"`
	ShareCertificateNo    string  `json:"share_certificate_no,omitempty"`
	ElectricityConsumerNo string  `json:"electricity_consumer_no,omitempty"`
	WaterConnectionNo     string  `json:"water_connection_no,omitempty"`
	// What the flat is like (#354). The counts are pointers because none
	// recorded and none present are different answers to a renter.
	About          string   `json:"about,omitempty"`
	Features       []string `json:"features,omitempty"`
	Bathrooms      *int     `json:"bathrooms,omitempty"`
	Balconies      *int     `json:"balconies,omitempty"`
	CoveredParking *int     `json:"covered_parking,omitempty"`
	Facing         string   `json:"facing,omitempty"`
	Furnishing     string   `json:"furnishing,omitempty"`
}

// unitTenancy is who is in the flat and on what terms. Absent, not empty, when
// nobody is: a screen that renders a rent of zero has said something false.
type unitTenancy struct {
	LeaseID   string `json:"lease_id"`
	State     string `json:"state"`
	Tenant    string `json:"tenant,omitempty"`
	Phone     string `json:"phone,omitempty"`
	RentMinor int64  `json:"rent_amount_minor"`
	DueDay    int    `json:"due_day,omitempty"`
	DueMinor  int64  `json:"due_amount_minor"`
	Starts    string `json:"starts,omitempty"`
	Ends      string `json:"ends,omitempty"`
	// LetFrom is set when the agreement is signed and the term has not begun.
	LetFrom string `json:"let_from,omitempty"`
}

type unitListing struct {
	ID             string  `json:"id"`
	State          string  `json:"state"`
	Headline       string  `json:"headline,omitempty"`
	Bedrooms       int     `json:"bedrooms,omitempty"`
	RentMinor      int64   `json:"rent_amount_minor"`
	DepositMinor   int64   `json:"deposit_amount_minor,omitempty"`
	CarpetAreaSqft float64 `json:"carpet_area_sqft,omitempty"`
	AvailableFrom  string  `json:"available_from,omitempty"`
}

type allottedUnit struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Kind string `json:"kind"`
}

// Unit is the flat a manager opens from the property record. Every field on it
// is a fact somebody standing in the flat needs: the size, the meter numbers,
// the rent, and — from the advert, because the register has no such column —
// how many bedrooms it was let as.
func (h *Handler) Unit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	u, err := h.properties.Unit(ctx, id)
	if errors.Is(err, propertyservice.ErrNoUnit) {
		writeError(w, http.StatusNotFound, "no such unit")
		return
	}
	if err != nil {
		h.log.Error("reading a unit", "unit", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the unit")
		return
	}

	p, err := h.properties.Get(ctx, u.PropertyID)
	if err != nil {
		h.log.Error("reading the property a unit is in", "unit", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the property")
		return
	}

	out := map[string]any{
		"unit": unitRecord{
			ID: u.ID, Code: u.Code, Kind: u.Kind, Floor: u.Floor, Occupancy: u.Occupancy,
			CarpetAreaSqft: u.CarpetSqf, BuiltupAreaSqft: u.BuiltupSqf,
			ShareCertificateNo:    u.ShareCertificateNo,
			ElectricityConsumerNo: u.ElectricityNo, WaterConnectionNo: u.WaterNo,
			About: u.About, Features: u.Features, Bathrooms: u.Bathrooms,
			Balconies: u.Balconies, CoveredParking: u.CoveredParking,
			Facing: u.Facing, Furnishing: u.Furnishing,
		},
		"property": propertyResponse{
			ID: p.ID, Code: p.Code, Name: p.Name, Kind: p.Kind,
			AddressLine1: p.AddressLine1, Locality: p.Locality, City: p.City, UnitCount: p.UnitCount,
			About: p.About, Amenities: p.Amenities,
		},
	}

	today := effective.DateOf(h.now(), ist)
	if t := h.unitTenancy(r, u.ID, today); t != nil {
		out["tenancy"] = t
	}
	if l := h.unitListing(r, u.ID); l != nil {
		out["listing"] = l
	}

	allotted, err := h.properties.Ancillaries(ctx, u.ID)
	if err != nil {
		h.log.Warn("reading what is allotted to a unit", "unit", id, "error", err)
	}
	rows := make([]allottedUnit, 0, len(allotted))
	for _, a := range allotted {
		rows = append(rows, allottedUnit{ID: a.ID, Code: a.Code, Kind: a.Kind})
	}
	out["ancillaries"] = rows

	writeJSON(w, http.StatusOK, out)
}

// unitTenancy describes the tenancy on a unit — the live one, or the signed one
// whose term has not begun. Nil when the flat is genuinely free.
func (h *Handler) unitTenancy(r *http.Request, unitID string, today effective.Date) *unitTenancy {
	ctx := r.Context()

	leaseID, err := h.leases.ActiveOnUnit(ctx, unitID, today)
	if err != nil {
		h.log.Warn("reading the tenancy on a unit", "unit", unitID, "error", err)
	}
	on, letFrom := today, effective.Date{}
	if leaseID == "" {
		next, from, err := h.leases.NextOnUnit(ctx, unitID, today)
		if err != nil {
			h.log.Warn("reading the next tenancy on a unit", "unit", unitID, "error", err)
			return nil
		}
		if next == "" {
			return nil
		}
		leaseID, on, letFrom = next, from, from
	}

	t, err := h.leases.Tenancy(ctx, leaseID, on)
	if err != nil {
		h.log.Warn("describing the tenancy on a unit", "lease", leaseID, "error", err)
		return nil
	}
	item := &unitTenancy{
		LeaseID: leaseID, State: string(t.State), RentMinor: t.RentMinor, DueDay: t.DueDay,
	}
	if !letFrom.Zero() {
		item.LetFrom = letFrom.String()
	}
	if !t.Term.From().Zero() {
		item.Starts = t.Term.From().String()
	}
	if !t.Term.To().Zero() {
		item.Ends = t.Term.To().String()
	}
	if tenantParty, _, err := h.leases.PartiesOf(ctx, leaseID, on); err == nil && tenantParty != "" {
		if profile, err := h.residents.Profile(ctx, tenantParty); err == nil {
			item.Tenant = profile.DisplayName
		}
		if phone, _, err := h.residents.Contact(ctx, tenantParty); err == nil {
			item.Phone = phone
		}
		if due, err := h.statements.Position(ctx, leaseID, tenantParty); err == nil {
			item.DueMinor = int64(due.Due)
		}
	}
	return item
}

// unitListing is the advert on the flat, if there is one. Optional dependency:
// a build without the discovery service simply has no bedroom count to give.
func (h *Handler) unitListing(r *http.Request, unitID string) *unitListing {
	if h.listings == nil {
		return nil
	}
	l, err := h.listings.ForUnit(r.Context(), unitID)
	if err != nil {
		return nil
	}
	item := &unitListing{
		ID: l.ID, State: string(l.State), Headline: l.Headline, Bedrooms: l.Bedrooms,
		RentMinor: l.Costs.RentMinor, DepositMinor: l.Costs.DepositMinor,
		CarpetAreaSqft: l.CarpetAreaSqft,
	}
	if !l.AvailableFrom.Zero() {
		item.AvailableFrom = l.AvailableFrom.String()
	}
	return item
}
