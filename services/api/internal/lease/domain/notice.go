package domain

import (
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Notice is notice of ending served on a live tenancy. #239.
type Notice struct {
	By       Actor
	ServedOn effective.Date
	// MoveOutOn is the last day of occupancy plus one — the same exclusive
	// bound Termination.EffectiveOn uses.
	MoveOutOn effective.Date
}

// ServeNotice moves the tenancy to in_notice.
//
// Notice announces an ending and performs nothing: rent still accrues, and the
// termination that later records reality is a separate act. What is judged
// here is only whether the announcement is legal — who may serve, and whether
// the named day honours the notice period and falls within the term.
func (l Lease) ServeNotice(n Notice) (Lease, Event, error) {
	if !MayTransition(l.State, StateInNotice, n.By) {
		return l, "", fmt.Errorf("%w: a %s may not serve notice on a %s lease", ErrTransition, n.By, l.State)
	}
	if n.ServedOn.Zero() || n.MoveOutOn.Zero() {
		return l, "", fmt.Errorf("%w: notice must say when it is served and when occupancy ends", ErrTransition)
	}
	earliest := n.ServedOn.AddDays(l.NoticeDays)
	if n.MoveOutOn.Before(earliest) {
		return l, "", fmt.Errorf("%w: %d days of notice from %s reaches %s, and the move-out named is %s",
			ErrTransition, l.NoticeDays, n.ServedOn, earliest, n.MoveOutOn)
	}
	if !n.MoveOutOn.After(l.Term.From()) {
		return l, "", fmt.Errorf("%w: a tenancy starting %s cannot cease %s",
			ErrTransition, l.Term.From(), n.MoveOutOn)
	}
	if !l.Term.To().Zero() && n.MoveOutOn.After(l.Term.To()) {
		return l, "", fmt.Errorf("%w: the agreed term ends %s — that day needs no notice",
			ErrTransition, l.Term.To())
	}

	out := l
	out.State = StateInNotice
	return out, EventNoticeServed, nil
}
