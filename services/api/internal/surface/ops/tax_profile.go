package ops

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// TaxProfileRoutes mounts what the landlord has furnished for TDS purposes
// (#318). can_administer, like ownership: this decides the rate somebody else's
// rent is deducted at.
func (h *Handler) TaxProfileRoutes(r *authz.Registrar) {
	r.Handle("PUT /v1/ops/parties/{id}/tax-profile", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.RecordTaxProfile)
	r.Handle("GET /v1/ops/parties/{id}/tax-profile", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.TaxProfile)
}

// taxProfileRequest carries the identifiers whole, because the manager has just
// read them off a card. Nothing here is written down as it arrived.
type taxProfileRequest struct {
	Residency        string `json:"residency"`
	ResidenceCountry string `json:"residence_country"`

	PAN        string `json:"pan"`
	ForeignTIN string `json:"foreign_tin"`

	TRCNumber      string `json:"trc_number"`
	TRCValidFrom   string `json:"trc_valid_from"`
	TRCValidTo     string `json:"trc_valid_to"`
	ForeignAddress string `json:"foreign_address"`
	ForeignEmail   string `json:"foreign_email"`
	ForeignPhone   string `json:"foreign_phone"`

	Source    string `json:"source"`
	ValidFrom string `json:"valid_from"`
}

type taxProfileResponse struct {
	PartyID           string `json:"party_id"`
	Residency         string `json:"residency"`
	ResidenceCountry  string `json:"residence_country"`
	PANFurnished      bool   `json:"pan_furnished"`
	ForeignTINMasked  string `json:"foreign_tin_masked"`
	TRCNumber         string `json:"trc_number,omitempty"`
	TRCValidTo        string `json:"trc_valid_to,omitempty"`
	Rule37BCFurnished bool   `json:"rule_37bc_furnished"`
	Source            string `json:"source"`
	ValidFrom         string `json:"valid_from"`
}

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// taxProfileFrom is the boundary. Identifiers come in whole and leave masked or
// as a boolean, and every fact the schema has a CHECK on is refused here first —
// a constraint violation reaches the manager with no field attached to it.
func taxProfileFrom(req taxProfileRequest) (identitystore.TaxProfile, error) {
	var none identitystore.TaxProfile

	residency := strings.TrimSpace(req.Residency)
	if residency != "resident" && residency != "non_resident" {
		return none, fmt.Errorf("residency must be resident or non_resident")
	}
	country := strings.ToUpper(strings.TrimSpace(req.ResidenceCountry))
	if !countryCode.MatchString(country) {
		return none, fmt.Errorf("residence_country must be a two-letter country code")
	}
	if (residency == "non_resident") != (country != "IN") {
		return none, fmt.Errorf(
			"residency and residence_country disagree, and together they select the section")
	}
	if strings.TrimSpace(req.Source) == "" {
		return none, fmt.Errorf("source must say who furnished this")
	}
	if _, err := time.Parse("2006-01-02", strings.TrimSpace(req.ValidFrom)); err != nil {
		return none, fmt.Errorf("valid_from must be a date, as YYYY-MM-DD")
	}

	// The PAN is checked and then discarded: only whether one was furnished is
	// recorded here, because that is what section 206AA turns on.
	furnished := false
	if pan := strings.TrimSpace(req.PAN); pan != "" {
		if err := pii.Validate(pii.KindPAN, pii.NewSecret(pan)); err != nil {
			return none, fmt.Errorf("pan is not a permanent account number")
		}
		furnished = true
	}

	return identitystore.TaxProfile{
		Residency:        residency,
		ResidenceCountry: country,
		PANFurnished:     furnished,
		ForeignTINMasked: maskTIN(req.ForeignTIN),
		TRCNumber:        strings.TrimSpace(req.TRCNumber),
		TRCValidFrom:     strings.TrimSpace(req.TRCValidFrom),
		TRCValidTo:       strings.TrimSpace(req.TRCValidTo),
		ForeignAddress:   strings.TrimSpace(req.ForeignAddress),
		ForeignEmail:     strings.TrimSpace(req.ForeignEmail),
		ForeignPhone:     strings.TrimSpace(req.ForeignPhone),
		Source:           strings.TrimSpace(req.Source),
		ValidFrom:        strings.TrimSpace(req.ValidFrom),
	}, nil
}

// A foreign TIN has no shape common to every jurisdiction, so it cannot go
// through pii.Mask, which validates against one. The rule is the same: keep the
// last four so the owner can recognise which document it came from, and nothing
// before them.
func maskTIN(tin string) string {
	raw := strings.ToUpper(strings.TrimSpace(tin))
	if raw == "" {
		return ""
	}
	const keep = 4
	if len(raw) <= keep*2 {
		return strings.Repeat(string(pii.MaskPrefix), len(raw))
	}
	return strings.Repeat(string(pii.MaskPrefix), len(raw)-keep) + raw[len(raw)-keep:]
}

// RecordTaxProfile writes what the landlord has furnished, from a date. It is a
// PUT because filing it twice for the same day is a correction, not a second
// truth about the same period.
func (h *Handler) RecordTaxProfile(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	party := r.PathValue("id")
	var req taxProfileRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	profile, err := taxProfileFrom(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.owners.RecordTaxProfile(r.Context(), party, profile); err != nil {
		// The profile is not logged: it came in holding a PAN.
		h.log.Error("recording a tax profile", "party", party, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "the tax profile was refused")
		return
	}
	h.readBackTaxProfile(w, r, party)
}

// TaxProfile reads what was furnished, as of today or an asked-for date.
func (h *Handler) TaxProfile(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	h.readBackTaxProfile(w, r, r.PathValue("id"))
}

func (h *Handler) readBackTaxProfile(w http.ResponseWriter, r *http.Request, party string) {
	on := h.now()
	if asked := r.URL.Query().Get("on"); asked != "" {
		parsed, err := time.Parse("2006-01-02", asked)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "on must be a date, as YYYY-MM-DD")
			return
		}
		on = parsed
	}

	got, err := h.owners.TaxProfile(r.Context(), party, on)
	switch {
	case errors.Is(err, identitystore.ErrNoTaxProfile):
		writeError(w, http.StatusNotFound, "nobody has asked this owner what they have furnished")
		return
	case err != nil:
		h.log.Error("reading a tax profile", "party", party, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the tax profile")
		return
	}
	writeJSON(w, http.StatusOK, taxProfileResponse{
		PartyID: party, Residency: got.Residency, ResidenceCountry: got.ResidenceCountry,
		PANFurnished: got.PANFurnished, ForeignTINMasked: got.ForeignTINMasked,
		TRCNumber: got.TRCNumber, TRCValidTo: got.TRCValidTo,
		Rule37BCFurnished: got.Rule37BCFurnished, Source: got.Source, ValidFrom: got.ValidFrom,
	})
}
