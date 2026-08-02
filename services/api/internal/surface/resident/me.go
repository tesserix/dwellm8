package resident

import (
	"net/http"

	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
)

// meResponse is the signed-in renter as the profile screen shows them. Contact
// details are what identity verified; there is no display name in this product
// yet — a landlord types a number, not a biography.
type meResponse struct {
	PartyID   string          `json:"party_id"`
	Phone     string          `json:"phone,omitempty"`
	Email     string          `json:"email,omitempty"`
	Tenancies []meTenancyItem `json:"tenancies"`
}

type meTenancyItem struct {
	LeaseID      string `json:"lease_id"`
	Organisation string `json:"organisation"`
	State        string `json:"state"`
}

// Me answers who this session is.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	session, ok := identityservice.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	out := meResponse{
		PartyID: session.PartyID, Phone: session.Phone,
		Tenancies: make([]meTenancyItem, 0, len(session.Residencies)),
	}
	if h.identity != nil {
		// Best-effort: the phone from the session already identifies them, and
		// a profile screen that 500s over a missing email helps nobody.
		if phone, email, err := h.identity.Contact(r.Context(), session.PartyID); err == nil {
			out.Email = email
			if out.Phone == "" {
				out.Phone = phone
			}
		}
	}
	for _, res := range session.Residencies {
		out.Tenancies = append(out.Tenancies, meTenancyItem{
			LeaseID: res.LeaseID, Organisation: res.Organisation, State: res.State,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
