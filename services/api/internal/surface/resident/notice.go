package resident

import (
	"errors"
	"net/http"

	leasedomain "github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

type noticeRequest struct {
	// MoveOutOn is the last day of occupancy plus one, ISO date. The server
	// refuses a date inside the notice period — the app shows the earliest
	// legal day before anything is sent.
	MoveOutOn string `json:"move_out_on"`
}

// ServeNotice records the renter's notice to vacate. #239.
//
// Served the moment it is posted: there is no draft state on the server,
// because a "draft notice" the manager can already see is indistinguishable
// from a served one. The app carries the review step.
func (h *Handler) ServeNotice(w http.ResponseWriter, r *http.Request) {
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	var req noticeRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	moveOut, err := effective.ParseDate(req.MoveOutOn)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "move_out_on must be a date, YYYY-MM-DD")
		return
	}

	ctx := session.Scope(r.Context(), res)
	today := effective.DateOf(h.now(), ist)

	t, err := h.leases.ServeNotice(ctx, res.LeaseID, leasedomain.Notice{
		By: leasedomain.ActorTenant, ServedOn: today, MoveOutOn: moveOut,
	})
	switch {
	case errors.Is(err, leasedomain.ErrTransition):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case err != nil:
		h.fail(w, "serving notice", res.LeaseID, err)
		return
	}

	out := present(t, res, today)
	writeJSON(w, http.StatusOK, out)
}
