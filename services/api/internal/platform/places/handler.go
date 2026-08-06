package places

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// Handler is the one endpoint every address field on every surface talks to.
type Handler struct {
	lookup Provider
	log    *slog.Logger
}

func NewHandler(l Provider, log *slog.Logger) *Handler {
	return &Handler{lookup: l, log: log}
}

// Routes mounts it open: the subject is a public geocoder, so no id of this
// system's own widens what an authenticated caller can reach through it.
func (h *Handler) Routes(r *authz.Registrar) {
	r.Open("GET /v1/places/autocomplete",
		"the subject is a public geocoder — no row of this system's is reachable through it",
		h.Autocomplete)
}

func (h *Handler) Autocomplete(w http.ResponseWriter, r *http.Request) {
	out, err := h.lookup.Suggest(r.Context(), r.URL.Query().Get("q"))
	if errors.Is(err, ErrUnavailable) {
		write(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Address lookup is unavailable just now — enter the address by hand.",
			"code":  "geocoder_unavailable",
		})
		return
	}
	if err != nil {
		h.log.Error("address lookup", "error", err)
		write(w, http.StatusInternalServerError, map[string]any{"error": "could not look that address up"})
		return
	}
	if out == nil {
		out = []Suggestion{}
	}
	write(w, http.StatusOK, map[string]any{"suggestions": out})
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
