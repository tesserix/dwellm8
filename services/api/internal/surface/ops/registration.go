package ops

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/identity/domain/registration"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// WithRegistrations wires the firm's own statutory registration (#282).
func (h *Handler) WithRegistrations(r *identityservice.Registrations) *Handler {
	h.registrations = r
	return h
}

// RegistrationRoutes mounts it. can_manage rather than can_view: this is the
// firm's own identity, and it decides whether it may hold a mandate at all.
func (h *Handler) RegistrationRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/registration", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.Registration)
	r.Handle("PUT /v1/ops/registration", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.SaveRegistration)
	r.Handle("POST /v1/ops/registration/authorities", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.FileRegistration)
	r.Handle("POST /v1/ops/registration/documents", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.RecordDocument)
}

type requirementResponse struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Label   string `json:"label"`
	Why     string `json:"why"`
	Expires bool   `json:"expires,omitempty"`
}

type authorityResponse struct {
	ID        string `json:"id"`
	Authority string `json:"authority"`
	StateCode string `json:"state_code"`
	Number    string `json:"number"`
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to"`
}

type registrationResponse struct {
	LegalName    string `json:"legal_name"`
	TradeName    string `json:"trade_name,omitempty"`
	Constitution string `json:"constitution"`
	// The mask, never the number. ADR-0013 §2.
	PANMasked    string `json:"pan_masked,omitempty"`
	TAN          string `json:"tan,omitempty"`
	GSTIN        string `json:"gstin,omitempty"`
	RegistrarID  string `json:"registrar_id,omitempty"`
	AddressLine1 string `json:"address_line1,omitempty"`
	AddressLine2 string `json:"address_line2,omitempty"`
	Locality     string `json:"locality,omitempty"`
	City         string `json:"city,omitempty"`
	StateCode    string `json:"state_code,omitempty"`
	PINCode      string `json:"pin_code,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
	ContactPhone string `json:"contact_phone,omitempty"`
	State        string `json:"state"`

	Authorities []authorityResponse   `json:"authorities"`
	Required    []requirementResponse `json:"required"`
	Outstanding []requirementResponse `json:"outstanding"`
	Fields      []string              `json:"fields"`
	MayManage   bool                  `json:"may_manage"`
}

// Registration is the whole checklist in one read.
func (h *Handler) Registration(w http.ResponseWriter, r *http.Request) {
	firm, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	standing, err := h.registrations.Standing(r.Context(), firm, h.now())
	if err != nil {
		h.log.Error("reading the firm registration", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the registration")
		return
	}

	out := registrationResponse{
		LegalName: standing.Profile.LegalName, TradeName: standing.Profile.TradeName,
		Constitution: standing.Profile.Constitution, PANMasked: standing.Profile.PANMasked,
		TAN: standing.Profile.TAN, GSTIN: standing.Profile.GSTIN,
		RegistrarID: standing.Profile.RegistrarID, AddressLine1: standing.Profile.AddressLine1,
		AddressLine2: standing.Profile.AddressLine2, Locality: standing.Profile.Locality,
		City: standing.Profile.City, StateCode: standing.Profile.StateCode,
		PINCode: standing.Profile.PINCode, ContactEmail: standing.Profile.ContactEmail,
		ContactPhone: standing.Profile.ContactPhone, State: standing.Profile.State,
		MayManage:   standing.MayManage,
		Authorities: make([]authorityResponse, 0, len(standing.Registrations)),
		Required:    make([]requirementResponse, 0, len(standing.Required)),
		Outstanding: make([]requirementResponse, 0, len(standing.Outstanding)),
		Fields:      make([]string, 0, len(standing.Fields)),
	}
	if out.State == "" {
		out.State = "draft"
	}
	for _, a := range standing.Registrations {
		out.Authorities = append(out.Authorities, authorityResponse{
			ID: a.ID, Authority: a.Authority, StateCode: a.StateCode, Number: a.Number,
			ValidFrom: a.ValidFrom.Format(time.DateOnly), ValidTo: a.ValidTo.Format(time.DateOnly),
		})
	}
	for _, req := range standing.Required {
		out.Required = append(out.Required, requirementResponse{
			Kind: string(req.Kind), Subject: string(req.Subject),
			Label: req.Label, Why: req.Why, Expires: req.Expires})
	}
	for _, req := range standing.Outstanding {
		out.Outstanding = append(out.Outstanding, requirementResponse{
			Kind: string(req.Kind), Subject: string(req.Subject),
			Label: req.Label, Why: req.Why, Expires: req.Expires})
	}
	for _, f := range standing.Fields {
		out.Fields = append(out.Fields, string(f))
	}
	writeJSON(w, http.StatusOK, out)
}

type saveRegistrationRequest struct {
	LegalName    string `json:"legal_name"`
	TradeName    string `json:"trade_name"`
	Constitution string `json:"constitution"`
	// The whole PAN, which is masked here and never stored.
	PAN          string `json:"pan"`
	TAN          string `json:"tan"`
	GSTIN        string `json:"gstin"`
	RegistrarID  string `json:"registrar_id"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2"`
	Locality     string `json:"locality"`
	City         string `json:"city"`
	StateCode    string `json:"state_code"`
	PINCode      string `json:"pin_code"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
}

func (h *Handler) SaveRegistration(w http.ResponseWriter, r *http.Request) {
	firm, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	var req saveRegistrationRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send the firm's details")
		return
	}
	if strings.TrimSpace(req.LegalName) == "" {
		writeError(w, http.StatusUnprocessableEntity,
			"send legal_name — it is the name every registration is held against")
		return
	}

	profile := identityservice.FirmProfile{
		LegalName: strings.TrimSpace(req.LegalName), TradeName: strings.TrimSpace(req.TradeName),
		Constitution: strings.TrimSpace(req.Constitution),
		TAN:          strings.ToUpper(strings.TrimSpace(req.TAN)),
		GSTIN:        strings.ToUpper(strings.TrimSpace(req.GSTIN)),
		RegistrarID:  strings.ToUpper(strings.TrimSpace(req.RegistrarID)),
		AddressLine1: strings.TrimSpace(req.AddressLine1), AddressLine2: strings.TrimSpace(req.AddressLine2),
		Locality: strings.TrimSpace(req.Locality), City: strings.TrimSpace(req.City),
		StateCode: strings.ToUpper(strings.TrimSpace(req.StateCode)), PINCode: strings.TrimSpace(req.PINCode),
		ContactEmail: strings.TrimSpace(req.ContactEmail), ContactPhone: strings.TrimSpace(req.ContactPhone),
	}
	// One field named, rather than three to go and check (#287).
	for _, f := range []struct {
		field registration.Field
		value string
	}{
		{registration.FieldGSTIN, profile.GSTIN},
		{registration.FieldTAN, profile.TAN},
		{registration.FieldPIN, profile.PINCode},
	} {
		if why := registration.Refuse(f.field, f.value); why != "" {
			writeFieldError(w, http.StatusUnprocessableEntity, string(f.field), why)
			return
		}
	}
	if pan := strings.TrimSpace(req.PAN); pan != "" {
		masked, err := pii.Mask(pii.KindPAN, pii.NewSecret(pan))
		if err != nil {
			writeFieldError(w, http.StatusUnprocessableEntity, string(registration.FieldPAN),
				"that PAN is not a PAN — ten characters, five letters, four digits, a letter")
			return
		}
		profile.PANMasked = masked
	}
	if profile.Constitution == "" {
		profile.Constitution = "proprietorship"
	}
	// Notices under the Act are served at this address, so it is never left to
	// whatever the client had in memory — the caller's own is known (#286).
	if profile.ContactEmail == "" {
		if p, ok := auth.From(r.Context()); ok {
			profile.ContactEmail = p.Email
		}
	}

	if err := h.registrations.Save(r.Context(), firm, profile); err != nil {
		h.log.Error("saving the firm registration", "error", err)
		writeError(w, http.StatusUnprocessableEntity, "those details were refused — check the PAN, GSTIN and PIN code")
		return
	}
	h.Registration(w, r)
}

type fileRegistrationRequest struct {
	Authority string `json:"authority"`
	StateCode string `json:"state_code"`
	Number    string `json:"number"`
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to"`
}

// FileRegistration records one state's agent registration.
func (h *Handler) FileRegistration(w http.ResponseWriter, r *http.Request) {
	firm, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	var req fileRegistrationRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send state_code, number, valid_from and valid_to")
		return
	}
	from, err1 := time.Parse(time.DateOnly, req.ValidFrom)
	to, err2 := time.Parse(time.DateOnly, req.ValidTo)
	if err1 != nil || err2 != nil || !to.After(from) {
		writeError(w, http.StatusUnprocessableEntity,
			"send valid_from and valid_to as YYYY-MM-DD, and valid_to after valid_from")
		return
	}
	if strings.TrimSpace(req.Number) == "" || len(strings.TrimSpace(req.StateCode)) != 2 {
		writeError(w, http.StatusUnprocessableEntity, "send the registration number and the two-letter state code")
		return
	}

	if err := h.registrations.File(r.Context(), firm, identityservice.Authority{
		Authority: strings.TrimSpace(req.Authority), StateCode: strings.ToUpper(strings.TrimSpace(req.StateCode)),
		Number: strings.TrimSpace(req.Number), ValidFrom: from, ValidTo: to,
	}); err != nil {
		h.log.Error("filing an agent registration", "error", err)
		writeError(w, http.StatusUnprocessableEntity,
			"that registration was refused — one authority per state, and the dates must not overlap an existing one")
		return
	}
	h.Registration(w, r)
}

type recordDocumentRequest struct {
	Kind        string `json:"kind"`
	ObjectPath  string `json:"object_path"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ExpiresOn   string `json:"expires_on"`
}

// RecordDocument notes an upload that has already reached the bucket.
func (h *Handler) RecordDocument(w http.ResponseWriter, r *http.Request) {
	firm, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	p, _ := auth.From(r.Context())
	var req recordDocumentRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send kind, object_path, filename and content_type")
		return
	}
	if req.Kind == "" || req.ObjectPath == "" || req.Filename == "" || req.ContentType == "" {
		writeError(w, http.StatusUnprocessableEntity, "send kind, object_path, filename and content_type")
		return
	}

	doc := identityservice.Document{
		Kind: req.Kind, ObjectPath: req.ObjectPath, Filename: req.Filename,
		ContentType: req.ContentType, UploadedBy: uploader(p),
	}
	if req.ExpiresOn != "" {
		on, err := time.Parse(time.DateOnly, req.ExpiresOn)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "send expires_on as YYYY-MM-DD")
			return
		}
		doc.ExpiresOn = on
	}

	if err := h.registrations.Record(r.Context(), firm, doc); err != nil {
		h.log.Error("recording a firm document", "error", err)
		writeError(w, http.StatusUnprocessableEntity, "that document was refused — check the kind")
		return
	}
	h.Registration(w, r)
}

func uploader(p auth.Principal) string {
	switch {
	case p.Email != "":
		return p.Email
	case p.Phone != "":
		return p.Phone
	default:
		return p.UID
	}
}
