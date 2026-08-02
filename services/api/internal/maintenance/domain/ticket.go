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
}

// TicketEvent is one line of the timeline.
type TicketEvent struct {
	At    time.Time
	Actor string
	Body  string
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
