package ops

import (
	"errors"
	"net/http"

	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leasedomain "github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	propertydomain "github.com/tesserix/dwellm8/services/api/internal/property/domain"
)

// WithOwners adds the identity module's owner-onboarding seam (#240).
func (h *Handler) WithOwners(o *identityservice.Owners) *Handler {
	h.owners = o
	return h
}

// OnboardingRoutes mounts the manager-led onboarding. can_administer: taking
// on an owner commits the firm to a mandate over somebody else's books, which
// is above day-to-day operation.
func (h *Handler) OnboardingRoutes(r *authz.Registrar) {
	r.Handle("POST /v1/ops/onboardings", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.OnboardOwner)
}

type onboardOwnerRequest struct {
	Owner struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
		Email string `json:"email"`
	} `json:"owner"`
	// OrganisationName is what the owner's books are called; derived from the
	// owner's name when empty.
	OrganisationName string `json:"organisation_name"`
	Property         struct {
		Code         string `json:"code"`
		Name         string `json:"name"`
		Kind         string `json:"kind"`
		AddressLine1 string `json:"address_line1"`
		AddressLine2 string `json:"address_line2"`
		Locality     string `json:"locality"`
		City         string `json:"city"`
		District     string `json:"district"`
		StateCode    string `json:"state_code"`
		Pin          string `json:"pin"`
	} `json:"property"`
	Units []struct {
		Code           string  `json:"code"`
		Kind           string  `json:"kind"`
		Floor          *int    `json:"floor"`
		CarpetAreaSqft float64 `json:"carpet_area_sqft"`
	} `json:"units"`
	// Tenancy seeds the first lease in the same breath — the details the
	// agreement runs on, with the tenant named by the number that will claim
	// their Live sign-in exactly as the owner claims Own.
	Tenancy *struct {
		UnitCode string `json:"unit_code"`
		Tenant   struct {
			Name  string `json:"name"`
			Phone string `json:"phone"`
			Email string `json:"email"`
		} `json:"tenant"`
		StartOn      string `json:"start_on"`
		EndOn        string `json:"end_on"`
		RentMinor    int64  `json:"rent_amount_minor"`
		DepositMinor int64  `json:"deposit_amount_minor"`
		DueDay       int    `json:"due_day"`
		NoticeDays   int    `json:"notice_days"`
		LockInUntil  string `json:"lock_in_until"`
		// The two TDS facts activation demands (ADR-0024), confirmed by the
		// manager on the review step. Defaults: an individual, resident owner
		// — the ordinary Indian landlord.
		DeductorClass     string `json:"deductor_class"`
		LandlordResidency string `json:"landlord_residency"`
	} `json:"tenancy"`
}

type onboardOwnerResponse struct {
	OwnerOrgID   string   `json:"owner_org_id"`
	OwnerPartyID string   `json:"owner_party_id"`
	GrantID      string   `json:"grant_id"`
	CreatedOrg   bool     `json:"created_organisation"`
	PropertyID   string   `json:"property_id,omitempty"`
	UnitIDs      []string `json:"unit_ids,omitempty"`
	LeaseID      string   `json:"lease_id,omitempty"`
	LeaseState   string   `json:"lease_state,omitempty"`
	// LeaseNote says why the tenancy stopped short of active, when it did —
	// a missing tax acknowledgement is the ordinary reason, and it is a
	// follow-up, not a failure.
	LeaseNote string `json:"lease_note,omitempty"`
}

// OnboardOwner is the manager taking on a new owner: their identity reserved
// against the phone that will claim it, their organisation, the firm's
// mandate over it, and the first property with its units — the owner sees all
// of it in the Own app the moment they sign in with that number.
func (h *Handler) OnboardOwner(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	firm, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	var req onboardOwnerRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	orgName := req.OrganisationName
	if orgName == "" {
		orgName = req.Owner.Name
	}

	onboarded, err := h.owners.PreOnboard(r.Context(), identityservice.OwnerOnboarding{
		FirmOrgID: firm.String(),
		OwnerName: req.Owner.Name, Phone: req.Owner.Phone, Email: req.Owner.Email,
		OrgName: orgName,
	})
	switch {
	case errors.Is(err, identitystore.ErrPhone):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case err != nil:
		h.log.Error("onboarding an owner", "error", err)
		writeError(w, http.StatusInternalServerError, "could not onboard the owner")
		return
	}

	out := onboardOwnerResponse{
		OwnerOrgID: onboarded.OrgID, OwnerPartyID: onboarded.PartyID,
		GrantID: onboarded.GrantID, CreatedOrg: onboarded.CreatedOrg,
	}

	// The property lands in the owner's organisation — that is what ownership
	// means here — under the mandate the same call just minted. A failure from
	// here on leaves an owner without a property, and the retry is idempotent
	// at every step above.
	unitByCode := map[string]string{}
	if req.Property.Name != "" || req.Property.Code != "" {
		ownerCtx := tenancy.With(r.Context(), tenancy.ID(onboarded.OrgID))
		propertyID, err := h.properties.Register(ownerCtx, propertydomain.PropertyDraft{
			Code: req.Property.Code, Name: req.Property.Name, Kind: req.Property.Kind,
			AddressLine1: req.Property.AddressLine1, AddressLine2: req.Property.AddressLine2,
			Locality: req.Property.Locality, City: req.Property.City,
			District: req.Property.District, StateCode: req.Property.StateCode, Pin: req.Property.Pin,
		})
		if err != nil {
			h.log.Error("registering the onboarded property", "owner_org", onboarded.OrgID, "error", err)
			writeError(w, http.StatusUnprocessableEntity,
				"the owner was onboarded, but the property was refused: "+err.Error())
			return
		}
		out.PropertyID = propertyID
		for _, u := range req.Units {
			unitID, err := h.properties.AddUnit(ownerCtx, propertyID, propertydomain.UnitDraft{
				Code: u.Code, Kind: u.Kind, Floor: u.Floor, CarpetAreaSqft: u.CarpetAreaSqft,
			})
			if err != nil {
				h.log.Error("adding an onboarded unit", "property", propertyID, "unit", u.Code, "error", err)
				writeError(w, http.StatusUnprocessableEntity,
					"the property was registered, but unit "+u.Code+" was refused: "+err.Error())
				return
			}
			out.UnitIDs = append(out.UnitIDs, unitID)
			unitByCode[u.Code] = unitID
		}

		// The first tenancy, in the same breath. The lease lives in the
		// owner's books like the property; the tenant is named by phone and
		// pre-registered the way ADR-0029 arrives backwards. Activation is
		// attempted and allowed to stop short — a missing tax acknowledgement
		// is a follow-up in the lease flow, not a failed onboarding.
		if t := req.Tenancy; t != nil && t.Tenant.Phone != "" {
			unitID, ok := unitByCode[t.UnitCode]
			if !ok {
				writeError(w, http.StatusUnprocessableEntity,
					"tenancy.unit_code must be one of the units being onboarded")
				return
			}
			draft, err := onboardingDraft(propertyID, unitID, req)
			if err != nil {
				writeError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			created, err := h.leases.Create(ownerCtx, draft)
			if err != nil {
				h.log.Error("creating the onboarded lease", "property", propertyID, "error", err)
				writeError(w, http.StatusUnprocessableEntity,
					"the property was registered, but the tenancy was refused: "+err.Error())
				return
			}
			out.LeaseID = created.ID
			out.LeaseState = string(created.Lease.State)
			if err := h.leases.Activate(ownerCtx, created.ID, leasedomain.ActorOwner); err != nil {
				out.LeaseNote = "created as a draft — " + err.Error()
			} else {
				out.LeaseState = string(leasedomain.StateActive)
			}
		}
	}

	writeJSON(w, http.StatusCreated, out)
}

// onboardingDraft builds the lease draft from the wizard's flat fields, the
// same conversions the lease surface makes.
func onboardingDraft(propertyID, unitID string, req onboardOwnerRequest) (leasedomain.Draft, error) {
	t := req.Tenancy
	start, err := effective.ParseDate(t.StartOn)
	if err != nil {
		return leasedomain.Draft{}, errors.New("tenancy.start_on must be a date, as YYYY-MM-DD")
	}
	var term effective.Interval
	if t.EndOn == "" {
		term, err = effective.Since(start)
	} else {
		var end effective.Date
		if end, err = effective.ParseDate(t.EndOn); err == nil {
			term, err = effective.Between(start, end)
		}
	}
	if err != nil {
		return leasedomain.Draft{}, errors.New("tenancy.end_on must be a date after start_on")
	}
	noticeDays := t.NoticeDays
	if noticeDays == 0 {
		noticeDays = 30
	}
	d := leasedomain.Draft{
		Property: propertyID, Unit: unitID,
		Term: term, NoticeDays: noticeDays,
		Terms: leasedomain.Terms{
			RentMinor: t.RentMinor, Cycle: leasedomain.Cycle("monthly"),
			DueDay: leasedomain.DueDay(t.DueDay), DepositMinor: t.DepositMinor,
			DepositHeldBy: leasedomain.DepositHolder("owner"),
		},
		Parties: []leasedomain.Party{{
			Role: leasedomain.PartyRole("tenant"),
			Name: t.Tenant.Name, Phone: t.Tenant.Phone, Email: t.Tenant.Email,
		}},
	}
	if t.LockInUntil != "" {
		if d.LockInUntil, err = effective.ParseDate(t.LockInUntil); err != nil {
			return leasedomain.Draft{}, errors.New("tenancy.lock_in_until must be a date, as YYYY-MM-DD")
		}
	}

	// The TDS facts, so activation can pass ADR-0024's gate. The manager
	// confirms them on the review step; the source names this flow.
	deductor := t.DeductorClass
	if deductor == "" {
		deductor = string(tds.IndividualNoAudit)
	}
	residency := t.LandlordResidency
	if residency == "" {
		residency = string(tds.Resident)
	}
	iv, err := effective.Since(start)
	if err != nil {
		return leasedomain.Draft{}, err
	}
	d.Tax, err = tds.NewHistory([]effective.Record[tds.Facts]{{
		ID: "1", Range: iv, Kind: effective.KindChange,
		Value: tds.Facts{
			Deductor: tds.DeductorClass(deductor), Residency: tds.Residency(residency),
			From: start, Source: "ops_onboarding",
			AcknowledgedBy: "manager", AcknowledgedOn: start,
		},
	}})
	if err != nil {
		return leasedomain.Draft{}, err
	}
	return d, nil
}
