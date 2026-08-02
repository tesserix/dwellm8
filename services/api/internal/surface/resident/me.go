package resident

import (
	"net/http"

	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
)

// meResponse is the signed-in renter as the profile screen shows them: the
// verified anchors, the name they chose for themselves (#240), and their
// tenancies.
type meResponse struct {
	PartyID     string          `json:"party_id"`
	Phone       string          `json:"phone,omitempty"`
	Email       string          `json:"email,omitempty"`
	DisplayName string          `json:"display_name,omitempty"`
	Tenancies   []meTenancyItem `json:"tenancies"`
}

type meTenancyItem struct {
	LeaseID      string `json:"lease_id"`
	Organisation string `json:"organisation"`
	State        string `json:"state"`
}

func (h *Handler) presentMe(r *http.Request, session identityservice.Session) meResponse {
	out := meResponse{
		PartyID: session.PartyID, Phone: session.Phone,
		Tenancies: make([]meTenancyItem, 0, len(session.Residencies)),
	}
	if h.identity != nil {
		// Best-effort: the phone from the session already identifies them, and
		// a profile screen that 500s over a missing email helps nobody.
		if p, err := h.identity.Profile(r.Context(), session.PartyID); err == nil {
			out.Email, out.DisplayName = p.Email, p.DisplayName
			if out.Phone == "" {
				out.Phone = p.Phone
			}
		}
	}
	for _, res := range session.Residencies {
		out.Tenancies = append(out.Tenancies, meTenancyItem{
			LeaseID: res.LeaseID, Organisation: res.Organisation, State: res.State,
		})
	}
	return out
}

// Me answers who this session is.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	session, ok := identityservice.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	writeJSON(w, http.StatusOK, h.presentMe(r, session))
}

type updateMeRequest struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// UpdateMe is the renter filling in their own PI after onboarding (#240).
// Only the self-served fields move; the verified phone is the anchor and
// never edited here.
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	session, ok := identityservice.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	if h.identity == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req updateMeRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if req.DisplayName == "" && req.Email == "" {
		writeError(w, http.StatusUnprocessableEntity, "send display_name or email — there is nothing else to change here")
		return
	}
	if _, err := h.identity.UpdateProfile(r.Context(), session.PartyID, req.DisplayName, req.Email); err != nil {
		h.log.Error("updating a renter's profile", "party", session.PartyID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not save your details")
		return
	}
	writeJSON(w, http.StatusOK, h.presentMe(r, session))
}
