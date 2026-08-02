package domain

import (
	"errors"
	"strings"
	"time"
)

// TicketCategory is what kind of work a ticket asks for.
type TicketCategory string

// Categories, matching the schema's CHECK.
var ticketCategories = map[TicketCategory]bool{
	"plumbing": true, "electrical": true, "carpentry": true, "appliance": true,
	"pest_control": true, "cleaning": true, "common_area": true, "other": true,
}

// Known reports whether this is a category the schema will accept.
func (c TicketCategory) Known() bool { return ticketCategories[c] }

// TicketStatus is where a ticket stands.
type TicketStatus string

const (
	TicketOpen         TicketStatus = "open"
	TicketAcknowledged TicketStatus = "acknowledged"
	TicketScheduled    TicketStatus = "scheduled"
	TicketInProgress   TicketStatus = "in_progress"
	TicketResolved     TicketStatus = "resolved"
	TicketCancelled    TicketStatus = "cancelled"
)

// ErrTicket is a ticket that cannot be raised as described.
var ErrTicket = errors.New("ticket: invalid")

// Ticket is one maintenance request on one tenancy.
type Ticket struct {
	ID       string
	TenantID string
	LeaseID  string
	// PropertyID and UnitID ride on the row for the delegation quad (ADR-0009).
	PropertyID string
	UnitID     string
	RaisedBy   string

	Category TicketCategory
	Title    string
	Body     string

	Status TicketStatus
	// Liability is empty until somebody assesses it — the app renders
	// "not yet assessed", never a guess.
	Liability       string
	LiabilityReason string
	Slot            string
	Vendor          string
	CostMinor       *int64

	CreatedAt time.Time
	UpdatedAt time.Time
	Events    []TicketEvent

	// UnitCode and PropertyName label the row on org-wide reads; empty on the
	// resident path, where the tenancy is already on screen.
	UnitCode     string
	PropertyName string
}

// TicketEvent is one line of the timeline.
type TicketEvent struct {
	At    time.Time
	Actor string
	Body  string
}

// TicketAction is a manager's move on a ticket (#237, ops leg).
type TicketAction string

const (
	TicketAcknowledge TicketAction = "acknowledge"
	TicketSchedule    TicketAction = "schedule"
	TicketAssess      TicketAction = "assess"
	TicketStart       TicketAction = "start"
	TicketResolve     TicketAction = "resolve"
	TicketCancel      TicketAction = "cancel"
)

// TicketPatch carries what an action sets alongside the status move.
type TicketPatch struct {
	Slot            string
	Vendor          string
	Liability       string
	LiabilityReason string
	CostMinor       *int64
	Note            string
}

func (t Ticket) settled() bool {
	return t.Status == TicketResolved || t.Status == TicketCancelled
}

// Advance applies one manager action and returns the updated ticket with the
// timeline line the store appends. Assess is deliberately orthogonal: who
// pays is a judgement, not a stage, and it may land at any point before the
// ticket settles.
func (t Ticket) Advance(a TicketAction, p TicketPatch) (Ticket, string, error) {
	if t.settled() {
		return t, "", errors.Join(ErrTicket, errors.New("that ticket is settled"))
	}
	out := t
	line := ""
	switch a {
	case TicketAcknowledge:
		if t.Status != TicketOpen {
			return t, "", errors.Join(ErrTicket, errors.New("only an open ticket is acknowledged"))
		}
		out.Status = TicketAcknowledged
		line = "Acknowledged by your manager"
	case TicketSchedule:
		if p.Slot == "" {
			return t, "", errors.Join(ErrTicket, errors.New("a schedule names a slot"))
		}
		out.Status = TicketScheduled
		out.Slot = p.Slot
		if p.Vendor != "" {
			out.Vendor = p.Vendor
		}
		line = "Visit scheduled: " + p.Slot
		if out.Vendor != "" {
			line += " — " + out.Vendor
		}
	case TicketAssess:
		if p.Liability != "owner" && p.Liability != "tenant" && p.Liability != "shared" {
			return t, "", errors.Join(ErrTicket, errors.New("liability is owner, tenant or shared"))
		}
		if strings.TrimSpace(p.LiabilityReason) == "" {
			return t, "", errors.Join(ErrTicket, errors.New("an assessment says why — it is the first question in any dispute"))
		}
		out.Liability, out.LiabilityReason = p.Liability, p.LiabilityReason
		if p.CostMinor != nil {
			out.CostMinor = p.CostMinor
		}
		line = "Liability assessed — " + p.Liability + "-borne"
	case TicketStart:
		out.Status = TicketInProgress
		line = "Work started"
	case TicketResolve:
		out.Status = TicketResolved
		line = "Resolved"
	case TicketCancel:
		out.Status = TicketCancelled
		line = "Cancelled"
	default:
		return t, "", errors.Join(ErrTicket, errors.New("unknown action"))
	}
	if p.Note != "" {
		line += ": " + p.Note
	}
	return out, line, nil
}

// ValidateRaise refuses a ticket the schema would refuse, with a message the
// person who typed it can act on.
func (t Ticket) ValidateRaise() error {
	if !t.Category.Known() {
		return errors.Join(ErrTicket, errors.New("unknown category"))
	}
	if strings.TrimSpace(t.Title) == "" {
		return errors.Join(ErrTicket, errors.New("a title is required"))
	}
	if t.LeaseID == "" || t.PropertyID == "" || t.UnitID == "" || t.RaisedBy == "" {
		return errors.Join(ErrTicket, errors.New("the tenancy is not resolved"))
	}
	return nil
}
