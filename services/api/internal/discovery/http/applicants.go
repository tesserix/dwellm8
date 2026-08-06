package http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
)

// The applicant pack (#258): the manager's side of what an Indian landlord
// actually asks before handing over keys. A save is a correction, never an
// overwrite, so the pack a decline was taken on stays readable (ADR-0008).

// ApplicantsHandler serves the pack behind the owner's mandate.
type ApplicantsHandler struct {
	packs *store.Applicants
	log   *slog.Logger
}

// NewApplicants builds the handler.
func NewApplicants(p *store.Applicants, log *slog.Logger) *ApplicantsHandler {
	return &ApplicantsHandler{packs: p, log: log}
}

// OwnerRoutes mounts the pack under the application it hangs off.
func (h *ApplicantsHandler) OwnerRoutes(r *ginx.Registrar) {
	view := authz.Check{Relation: "can_view", Object: authz.Organisation()}
	op := authz.Check{Relation: "can_operate", Object: authz.Organisation()}
	r.Handle(http.MethodGet, "/v1/ops/applications/:id/profile", view, h.Get)
	r.Handle(http.MethodPut, "/v1/ops/applications/:id/profile", op, h.Save)
	r.Handle(http.MethodPost, "/v1/ops/applications/:id/profile/submit", op, h.Submit)
	r.Handle(http.MethodPut, "/v1/ops/applications/:id/profile/people", op, h.SavePeople)
}

type profileRequest struct {
	FullName     string `json:"full_name" binding:"required,max=200"`
	DateOfBirth  string `json:"date_of_birth" binding:"omitempty,len=10"`
	Nationality  string `json:"nationality" binding:"omitempty,len=2"`
	TaxResidency string `json:"tax_residency" binding:"omitempty,oneof=resident non_resident"`
	Occupants    int    `json:"occupants" binding:"omitempty,min=1,max=20"`
	Pets         bool   `json:"pets"`
	PetsNote     string `json:"pets_note" binding:"omitempty,max=500"`

	People []personRequest `json:"people" binding:"omitempty,dive"`
}

type personRequest struct {
	Role         string `json:"role" binding:"required,oneof=co_applicant dependant guarantor"`
	FullName     string `json:"full_name" binding:"required,max=200"`
	Relationship string `json:"relationship" binding:"omitempty,max=100"`
	DateOfBirth  string `json:"date_of_birth" binding:"omitempty,len=10"`
	// Not the validator's e164 tag: it accepts a bare ten-digit number, which
	// the column rejects. The store owns the one rule (ADR-0029).
	Phone string `json:"phone" binding:"omitempty,max=16"`
	Email string `json:"email" binding:"omitempty,email"`
}

// Save writes the pack, superseding whatever stood before it. The household
// travels with the pack when given, because the two are edited on one screen.
func (h *ApplicantsHandler) Save(c *gin.Context) {
	var req profileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	dob, err := optionalDate(req.DateOfBirth)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "date_of_birth must be a date, as YYYY-MM-DD"})
		return
	}
	people, err := people(req.People)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	saved, err := h.packs.SaveProfile(c.Request.Context(), c.Param("id"), store.Profile{
		FullName: req.FullName, DateOfBirth: dob, Nationality: req.Nationality,
		TaxResidency: req.TaxResidency, Occupants: req.Occupants,
		Pets: req.Pets, PetsNote: req.PetsNote,
	})
	if err != nil {
		h.fail(c, "save the pack", err)
		return
	}
	if req.People != nil {
		if err := h.packs.SavePeople(c.Request.Context(), c.Param("id"), people); err != nil {
			h.fail(c, "save the household", err)
			return
		}
	}
	c.JSON(http.StatusOK, profileView(saved, people))
}

// SavePeople replaces the household on its own.
func (h *ApplicantsHandler) SavePeople(c *gin.Context) {
	var req struct {
		People []personRequest `json:"people" binding:"dive"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	list, err := people(req.People)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if err := h.packs.SavePeople(c.Request.Context(), c.Param("id"), list); err != nil {
		h.fail(c, "save the household", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"people": personViews(list)})
}

// Get is the pack as it stands, half-finished or submitted.
func (h *ApplicantsHandler) Get(c *gin.Context) {
	p, err := h.packs.Profile(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "read the pack", err)
		return
	}
	household, err := h.packs.People(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "read the household", err)
		return
	}
	c.JSON(http.StatusOK, profileView(p, household))
}

// Submit closes the draft. Only a submitted pack is decidable.
func (h *ApplicantsHandler) Submit(c *gin.Context) {
	if err := h.packs.Submit(c.Request.Context(), c.Param("id")); err != nil {
		h.fail(c, "submit the pack", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "submitted"})
}

func (h *ApplicantsHandler) fail(c *gin.Context, what string, err error) {
	switch {
	case errors.Is(err, store.ErrNoProfile):
		c.JSON(http.StatusNotFound, gin.H{"error": "no pack has been started for this application"})
	case errors.Is(err, store.ErrNoApplication):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, store.ErrAlreadySubmitted):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, store.ErrPhone):
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "a phone number must include its country code, as +919847033222"})
	default:
		h.log.Error("the applicant pack: "+what, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not " + what})
	}
}

func people(in []personRequest) ([]store.Person, error) {
	out := make([]store.Person, 0, len(in))
	for _, p := range in {
		dob, err := optionalDate(p.DateOfBirth)
		if err != nil {
			return nil, errors.New("every date_of_birth must be a date, as YYYY-MM-DD")
		}
		out = append(out, store.Person{
			Role: p.Role, FullName: p.FullName, Relationship: p.Relationship,
			DateOfBirth: dob, Phone: p.Phone, Email: p.Email,
		})
	}
	return out, nil
}

func optionalDate(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func profileView(p store.Profile, household []store.Person) gin.H {
	out := gin.H{
		"id": p.ID, "application_id": p.ApplicationID, "full_name": p.FullName,
		"nationality": p.Nationality, "tax_residency": p.TaxResidency,
		"occupants": p.Occupants, "pets": p.Pets, "state": p.State,
		"people": personViews(household),
	}
	if p.DateOfBirth != nil {
		out["date_of_birth"] = p.DateOfBirth.Format("2006-01-02")
	}
	if p.PetsNote != "" {
		out["pets_note"] = p.PetsNote
	}
	if !p.SubmittedAt.IsZero() {
		out["submitted_at"] = p.SubmittedAt.UTC().Format(time.RFC3339)
	}
	if p.Corrects != "" {
		out["corrects"] = p.Corrects
	}
	return out
}

func personViews(list []store.Person) []gin.H {
	out := make([]gin.H, 0, len(list))
	for _, p := range list {
		v := gin.H{"role": p.Role, "full_name": p.FullName}
		if p.ID != "" {
			v["id"] = p.ID
		}
		if p.Relationship != "" {
			v["relationship"] = p.Relationship
		}
		if p.Phone != "" {
			v["phone"] = p.Phone
		}
		if p.Email != "" {
			v["email"] = p.Email
		}
		if p.DateOfBirth != nil {
			v["date_of_birth"] = p.DateOfBirth.Format("2006-01-02")
		}
		out = append(out, v)
	}
	return out
}
