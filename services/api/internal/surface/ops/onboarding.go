package ops

import (
	"errors"
	"net/http"
	"time"

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
		// OrgID names an owner the firm already acts for — the second and
		// every later property. Their identity is not re-entered, and the
		// mandate already held is the one it lands under.
		OrgID string `json:"org_id"`
		Name  string `json:"name"`
		Phone string `json:"phone"`
		Email string `json:"email"`
		// Self is the solo manager: they own this property themselves, so it
		// lands in their own books with no mandate between (#268).
		Self bool `json:"self"`
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
	onboarded, err := h.resolveOwner(r, firm.String(), req)
	switch {
	case errors.Is(err, identitystore.ErrNoMandate):
		writeError(w, http.StatusForbidden, "this firm does not act for that owner")
		return
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

		// Who the rent is credited to. Without this the billing run refuses the
		// tenancy for ever rather than attributing somebody's income to a party
		// it guessed (#302), and it refuses it silently, every half hour.
		startOn := ""
		if req.Tenancy != nil {
			startOn = req.Tenancy.StartOn
		}
		if err := h.properties.RecordOwnership(ownerCtx, propertyID, onboarded.PartyID,
			ownershipFrom(h.now(), startOn)); err != nil {
			h.log.Error("recording ownership of the onboarded property",
				"property", propertyID, "error", err)
			writeError(w, http.StatusUnprocessableEntity,
				"the property was registered, but its ownership was refused: "+err.Error())
			return
		}

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
			// The tenant acquires their identity here, weeks before they sign
			// in — the number on the agreement is what will claim it (ADR-0029).
			tenantParty, err := h.residents.PartyFor(r.Context(), t.Tenant.Phone)
			if err != nil {
				h.log.Error("pre-registering the onboarded tenant", "error", err)
				writeError(w, http.StatusUnprocessableEntity,
					"the property was registered, but the tenant was refused: "+err.Error())
				return
			}
			draft, err := onboardingDraft(onboarded.OrgID, tenantParty, propertyID, unitID, req)
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
			if err := h.leases.Offer(ownerCtx, created.ID, leasedomain.ActorOwner); err != nil {
				out.LeaseNote = "created as a draft — " + err.Error()
			} else if err := h.leases.Activate(ownerCtx, created.ID, leasedomain.ActorOwner); err != nil {
				out.LeaseNote = "created as a draft — " + err.Error()
			} else {
				out.LeaseState = string(leasedomain.StateActive)
			}
		}
	}

	writeJSON(w, http.StatusCreated, out)
}

// resolveOwner is whose books the property is about to land in: an owner the
// firm already acts for, named by organisation, or a new one reserved against
// the number that will claim their first Own sign-in.
func (h *Handler) resolveOwner(r *http.Request, firmOrgID string, req onboardOwnerRequest) (identityservice.OwnerOnboarded, error) {
	if req.Owner.Self || req.Owner.OrgID == firmOrgID {
		return h.owners.PreOnboard(r.Context(), identityservice.OwnerOnboarding{
			FirmOrgID: firmOrgID, SelfOwned: true,
			OwnerName: req.Owner.Name, Phone: req.Owner.Phone, Email: req.Owner.Email,
		})
	}
	if req.Owner.OrgID != "" {
		held, err := h.owners.Portfolio(r.Context(), firmOrgID, req.Owner.OrgID)
		if err != nil {
			return identityservice.OwnerOnboarded{}, err
		}
		// The owner already exists, so nothing is minted — but their party is
		// still what the next property's rent is credited to (#302).
		party, err := h.owners.OwnerParty(r.Context(), held.OwnerOrgID)
		if err != nil {
			return identityservice.OwnerOnboarded{}, err
		}
		return identityservice.OwnerOnboarded{
			OrgID: held.OwnerOrgID, PartyID: party, GrantID: held.GrantID}, nil
	}
	orgName := req.OrganisationName
	if orgName == "" {
		orgName = req.Owner.Name
	}
	return h.owners.PreOnboard(r.Context(), identityservice.OwnerOnboarding{
		FirmOrgID: firmOrgID,
		OwnerName: req.Owner.Name, Phone: req.Owner.Phone, Email: req.Owner.Email,
		OrgName: orgName,
	})
}

// onboardingDraft builds the lease draft from the wizard's flat fields, the
// same conversions the lease surface makes.
func onboardingDraft(ownerOrgID, tenantPartyID, propertyID, unitID string, req onboardOwnerRequest) (leasedomain.Draft, error) {
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
		TenantID: ownerOrgID,
		Property: propertyID, Unit: unitID,
		Term: term, NoticeDays: noticeDays,
		Terms: leasedomain.Terms{
			RentMinor: t.RentMinor, Cycle: leasedomain.Cycle("monthly"),
			DueDay: leasedomain.DueDay(t.DueDay), DepositMinor: t.DepositMinor,
			DepositHeldBy: leasedomain.DepositHolder("owner"),
		},
		Parties: []leasedomain.Party{{
			PartyID: tenantPartyID, Role: leasedomain.PartyRole("tenant"),
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

// ownershipFrom is the date the owner has held the property from, as far as
// this product can tell. Onboarding day, unless the tenancy being onboarded
// with it already started — a lease that began in March cannot be billed
// against ownership that begins today (#302).
func ownershipFrom(now time.Time, tenancyStart string) effective.Date {
	today := effective.DateOf(now, time.UTC)
	start, err := effective.ParseDate(tenancyStart)
	if err != nil || !start.Before(today) {
		return today
	}
	return start
}
