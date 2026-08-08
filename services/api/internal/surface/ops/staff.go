package ops

import (
	"encoding/json"
	"errors"
	"net/http"

	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The firm's own team (#353): the sub-managers it employs, what each may do,
// which buildings each is responsible for, and when they work.

// WithStaff adds the identity module's team seam (#353).
func (h *Handler) WithStaff(s *identityservice.Staff) *Handler {
	h.staff = s
	return h
}

// StaffRoutes mounts the team.
func (h *Handler) StaffRoutes(r *authz.Registrar) {
	manage := authz.Check{Relation: "can_manage", Object: authz.Organisation()}
	r.Handle("GET /v1/ops/staff", authz.Check{Relation: "can_view", Object: authz.Organisation()}, h.Team)
	r.Handle("POST /v1/ops/staff", manage, h.EmployStaff)
	r.Handle("POST /v1/ops/staff/roles", manage, h.SaveStaffRole)
	r.Handle("PATCH /v1/ops/staff/{id}", manage, h.UpdateStaff)
	r.Handle("POST /v1/ops/staff/{id}/assignments", manage, h.AssignProperty)
	r.Handle("DELETE /v1/ops/staff/assignments/{id}", manage, h.ReleaseAssignment)
	r.Handle("GET /v1/ops/staff/{id}/shifts", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Rota)
	r.Handle("PUT /v1/ops/staff/{id}/shifts", manage, h.SetRota)
}

type staffRoleResponse struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Permissions   []string `json:"permissions"`
	PropertyLimit int      `json:"property_limit"`
	People        int      `json:"people"`
}

// staffResponse is the employment record as the app reads it. The PAN is the
// mask and nothing else — the number is not held, so it cannot be returned.
type staffResponse struct {
	ID       string `json:"id"`
	PartyID  string `json:"party_id"`
	RoleID   string `json:"role_id,omitempty"`
	RoleName string `json:"role_name,omitempty"`

	FullName     string `json:"full_name"`
	EmployeeCode string `json:"employee_code,omitempty"`
	Designation  string `json:"designation,omitempty"`
	Email        string `json:"email,omitempty"`
	Phone        string `json:"phone,omitempty"`

	EmploymentType string `json:"employment_type"`
	JoinedOn       string `json:"joined_on"`
	ExitedOn       string `json:"exited_on,omitempty"`

	PANMasked      string `json:"pan_masked,omitempty"`
	SalaryMinor    int64  `json:"salary_minor,omitempty"`
	SalaryCurrency string `json:"salary_currency,omitempty"`
	PayFrequency   string `json:"pay_frequency,omitempty"`

	EmergencyName  string `json:"emergency_name,omitempty"`
	EmergencyPhone string `json:"emergency_phone,omitempty"`

	State         string `json:"state"`
	PropertyLimit int    `json:"property_limit"`
	Held          int    `json:"held"`
}

type assignmentResponse struct {
	ID           string `json:"id"`
	StaffID      string `json:"staff_id"`
	StaffName    string `json:"staff_name,omitempty"`
	PropertyID   string `json:"property_id"`
	PropertyName string `json:"property_name,omitempty"`
	ValidFrom    string `json:"valid_from,omitempty"`
}

type shiftResponse struct {
	Weekday  int    `json:"weekday"`
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
}

func asStaff(m identityservice.StaffMember) staffResponse {
	return staffResponse{
		ID: m.ID, PartyID: m.PartyID, RoleID: m.RoleID, RoleName: m.RoleName,
		FullName: m.FullName, EmployeeCode: m.EmployeeCode, Designation: m.Designation,
		Email: m.Email, Phone: m.Phone, EmploymentType: m.EmploymentType,
		JoinedOn: m.JoinedOn, ExitedOn: m.ExitedOn, PANMasked: m.PANMasked,
		SalaryMinor: m.SalaryMinor, SalaryCurrency: m.SalaryCurrency,
		PayFrequency: m.PayFrequency, EmergencyName: m.EmergencyName,
		EmergencyPhone: m.EmergencyPhone, State: m.State,
		PropertyLimit: m.PropertyLimit, Held: m.Held,
	}
}

// Team is the whole screen: roles, people and who holds what.
func (h *Handler) Team(w http.ResponseWriter, r *http.Request) {
	org, ok := h.firm(w, r)
	if !ok {
		return
	}
	team, err := h.staff.Team(r.Context(), org)
	if err != nil {
		h.log.Error("reading the firm's team", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the team")
		return
	}
	roles := make([]staffRoleResponse, 0, len(team.Roles))
	for _, role := range team.Roles {
		roles = append(roles, staffRoleResponse{
			ID: role.ID, Name: role.Name, Permissions: role.Permissions,
			PropertyLimit: role.PropertyLimit, People: role.People,
		})
	}
	members := make([]staffResponse, 0, len(team.Members))
	for _, m := range team.Members {
		members = append(members, asStaff(m))
	}
	held := make([]assignmentResponse, 0, len(team.Assignments))
	for _, a := range team.Assignments {
		held = append(held, assignmentResponse{
			ID: a.ID, StaffID: a.StaffID, StaffName: a.StaffName,
			PropertyID: a.PropertyID, PropertyName: a.PropertyName, ValidFrom: a.ValidFrom,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"roles": roles, "team": members, "assignments": held,
	})
}

type staffRoleRequest struct {
	Name          string   `json:"name"`
	Permissions   []string `json:"permissions"`
	PropertyLimit int      `json:"property_limit"`
}

// SaveStaffRole names a job and says how many properties it carries.
func (h *Handler) SaveStaffRole(w http.ResponseWriter, r *http.Request) {
	org, ok := h.firm(w, r)
	if !ok {
		return
	}
	var req staffRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a role")
		return
	}
	role, err := h.staff.SaveRole(r.Context(), org, identityservice.StaffRole{
		Name: req.Name, Permissions: req.Permissions, PropertyLimit: req.PropertyLimit,
	})
	if err != nil {
		h.staffError(w, "saving a role", err, "could not save the role")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"role": staffRoleResponse{
		ID: role.ID, Name: role.Name, Permissions: role.Permissions,
		PropertyLimit: role.PropertyLimit,
	}})
}

type employRequest struct {
	PartyID      string `json:"party_id"`
	RoleID       string `json:"role_id"`
	FullName     string `json:"full_name"`
	EmployeeCode string `json:"employee_code"`
	Designation  string `json:"designation"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`

	EmploymentType string `json:"employment_type"`
	JoinedOn       string `json:"joined_on"`

	// PAN arrives whole, once. What is kept is the mask (ADR-0013).
	PAN            string `json:"pan"`
	SalaryMinor    int64  `json:"salary_minor"`
	SalaryCurrency string `json:"salary_currency"`
	PayFrequency   string `json:"pay_frequency"`

	EmergencyName  string `json:"emergency_name"`
	EmergencyPhone string `json:"emergency_phone"`

	State         string `json:"state"`
	PropertyLimit int    `json:"property_limit"`
}

// EmployStaff puts a sub-manager on the firm's payroll.
func (h *Handler) EmployStaff(w http.ResponseWriter, r *http.Request) {
	org, ok := h.firm(w, r)
	if !ok {
		return
	}
	var req employRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a colleague")
		return
	}
	member, err := h.staff.Employ(r.Context(), org, identityservice.StaffMember{
		PartyID: req.PartyID, RoleID: req.RoleID, FullName: req.FullName,
		EmployeeCode: req.EmployeeCode, Designation: req.Designation,
		Email: req.Email, Phone: req.Phone, EmploymentType: req.EmploymentType,
		JoinedOn: req.JoinedOn, SalaryMinor: req.SalaryMinor,
		SalaryCurrency: req.SalaryCurrency, PayFrequency: req.PayFrequency,
		EmergencyName: req.EmergencyName, EmergencyPhone: req.EmergencyPhone,
		State: req.State, PropertyLimit: req.PropertyLimit,
	}, req.PAN)
	if err != nil {
		h.staffError(w, "employing a colleague", err, "could not add the colleague")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"member": asStaff(member)})
}

type updateStaffRequest struct {
	State         string `json:"state"`
	ExitedOn      string `json:"exited_on"`
	PropertyLimit int    `json:"property_limit"`
}

// UpdateStaff changes what a firm may change after somebody is employed.
func (h *Handler) UpdateStaff(w http.ResponseWriter, r *http.Request) {
	org, ok := h.firm(w, r)
	if !ok {
		return
	}
	var req updateStaffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a change")
		return
	}
	err := h.staff.Update(r.Context(), org, r.PathValue("id"), identityservice.Change{
		State: req.State, ExitedOn: req.ExitedOn, PropertyLimit: req.PropertyLimit,
	})
	if err != nil {
		h.staffError(w, "changing a colleague's terms", err, "could not save the change")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": true})
}

type assignRequest struct {
	PropertyID string `json:"property_id"`
}

// AssignProperty makes one colleague responsible for one building.
func (h *Handler) AssignProperty(w http.ResponseWriter, r *http.Request) {
	org, ok := h.firm(w, r)
	if !ok {
		return
	}
	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not an assignment")
		return
	}
	a, err := h.staff.Assign(r.Context(), org, r.PathValue("id"), req.PropertyID)
	if err != nil {
		h.staffError(w, "assigning a property", err, "could not assign the property")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"assignment": assignmentResponse{
		ID: a.ID, StaffID: a.StaffID, PropertyID: a.PropertyID,
	}})
}

// ReleaseAssignment hands a building back today.
func (h *Handler) ReleaseAssignment(w http.ResponseWriter, r *http.Request) {
	org, ok := h.firm(w, r)
	if !ok {
		return
	}
	if err := h.staff.Release(r.Context(), org, r.PathValue("id")); err != nil {
		h.staffError(w, "handing a property back", err, "could not hand the property back")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"released": true})
}

// Rota is one colleague's week.
func (h *Handler) Rota(w http.ResponseWriter, r *http.Request) {
	org, ok := h.firm(w, r)
	if !ok {
		return
	}
	week, err := h.staff.Rota(r.Context(), org, r.PathValue("id"))
	if err != nil {
		h.staffError(w, "reading a rota", err, "could not read the rota")
		return
	}
	out := make([]shiftResponse, 0, len(week))
	for _, sh := range week {
		out = append(out, shiftResponse{Weekday: sh.Weekday, StartsAt: sh.StartsAt, EndsAt: sh.EndsAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"shifts": out})
}

type rotaRequest struct {
	Shifts []shiftResponse `json:"shifts"`
}

// SetRota replaces the whole week, because that is how a rota is edited.
func (h *Handler) SetRota(w http.ResponseWriter, r *http.Request) {
	org, ok := h.firm(w, r)
	if !ok {
		return
	}
	var req rotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a rota")
		return
	}
	week := make([]identityservice.StaffShift, 0, len(req.Shifts))
	for _, sh := range req.Shifts {
		week = append(week, identityservice.StaffShift{
			Weekday: sh.Weekday, StartsAt: sh.StartsAt, EndsAt: sh.EndsAt,
		})
	}
	if err := h.staff.SetRota(r.Context(), org, r.PathValue("id"), week); err != nil {
		h.staffError(w, "setting a rota", err, "could not save the rota")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shifts": req.Shifts})
}

// staffError turns the service's refusals into answers a manager can act on.
// The cap and the held building are conflicts rather than server errors: the
// request was well formed, the firm just cannot have what it asked for.
func (h *Handler) staffError(w http.ResponseWriter, what string, err error, fallback string) {
	switch {
	case errors.Is(err, identityservice.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, errors.Unwrap(err).Error())
	case errors.Is(err, identityservice.ErrOverCap):
		writeError(w, http.StatusConflict,
			"that colleague already holds as many properties as their role allows — "+
				"hand one back, or raise what they carry")
	case errors.Is(err, identityservice.ErrPropertyHeld):
		writeError(w, http.StatusConflict,
			"another colleague is already responsible for that property")
	case errors.Is(err, identityservice.ErrNoStaff):
		writeError(w, http.StatusNotFound, "no such colleague")
	default:
		h.log.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, fallback)
	}
}

// firm is the organisation this request acts as.
func (h *Handler) firm(w http.ResponseWriter, r *http.Request) (tenancy.ID, bool) {
	org, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return "", false
	}
	return org, true
}
