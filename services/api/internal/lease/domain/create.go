package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

// Creating a lease. ADR-0010, and the two facts ADR-0024 demands before one can
// start.
//
// A draft is deliberately permissive and an activation is deliberately not. Two
// competing drafts on one flat are legitimate — that is what an offer is — and
// the moment one becomes a tenancy, nothing else may overlap it and the TDS path
// must be known. So Draft.Validate checks what a document needs, and Activate
// checks what a tenancy needs.

// EventCreated is published when a lease is drafted. It is not a transition
// event: nothing changed state, a thing came into existence, and the renewal
// reminder cares about that as much as it cares about the tenancy starting.
const EventCreated Event = "lease.created"

// PartyRole is why somebody is named on the lease.
type PartyRole string

const (
	// RoleTenant is a party liable for the rent. At least one is required.
	RoleTenant PartyRole = "tenant"
	// RoleGuarantor stands behind the rent without occupying.
	RoleGuarantor PartyRole = "guarantor"
	// RoleOccupant lives there without being liable — a spouse, a child, a
	// flatmate on somebody else's agreement. Recorded because police
	// verification and society records ask who is in the flat, not who signed.
	RoleOccupant PartyRole = "occupant"
)

// PartyRoles returns every role, ordered.
func PartyRoles() []PartyRole { return []PartyRole{RoleGuarantor, RoleOccupant, RoleTenant} }

func (r PartyRole) Valid() bool {
	switch r {
	case RoleTenant, RoleGuarantor, RoleOccupant:
		return true
	}
	return false
}

// Party is somebody named on the lease.
//
// Phone rather than email is the required contact: an Indian tenancy runs on a
// mobile number, and an email address is the optional one. No Aadhaar field
// exists here or anywhere — ADR-0013 §2, and the build fails on a struct field
// named after it.
type Party struct {
	PartyID string
	Role    PartyRole
	Name    string
	Phone   string
	Email   string
}

// phonePattern is an Indian mobile number in E.164, which is what every
// provider this platform talks to requires.
var phonePattern = regexp.MustCompile(`^\+91[6-9][0-9]{9}$`)

// Validate refuses a party that cannot be contacted or identified.
func (p Party) Validate() error {
	switch {
	case !p.Role.Valid():
		return fmt.Errorf("%w: %q is not a role on a lease", ErrDraft, p.Role)
	case strings.TrimSpace(p.Name) == "":
		return fmt.Errorf("%w: a %s with no name", ErrDraft, p.Role)
	case p.Role != RoleOccupant && !phonePattern.MatchString(p.Phone):
		// An occupant may be a child or an elderly parent with no phone of their
		// own. A tenant or a guarantor is somebody money and notice must reach.
		return fmt.Errorf("%w: %q is not an Indian mobile number in E.164 — a tenancy runs on a "+
			"number that can be reached", ErrDraft, p.Phone)
	case p.Email != "" && !strings.Contains(p.Email, "@"):
		return fmt.Errorf("%w: %q is not an email address", ErrDraft, p.Email)
	}
	return nil
}

// ErrDraft is a draft that cannot become a lease.
var ErrDraft = errors.New("lease: the draft is not complete enough to create")

// Draft is everything needed to create a lease.
type Draft struct {
	TenantID string
	Property string
	Unit     string

	// Term is the agreed period. Open-ended is a periodic tenancy and is
	// permitted; the eleven-month convention is a habit, not a rule.
	Term        effective.Interval
	NoticeDays  int
	LockInUntil effective.Date

	Terms   Terms
	Parties []Party

	// Tax is the deductor class and the landlord's residency (ADR-0024). It may
	// be empty on a draft and may not be empty on a tenancy.
	Tax tds.History

	// Renews names the lease this one succeeds, where it does.
	Renews string
}

// Validate refuses a draft that could not be written.
//
// It does not require the tax facts. A draft is a document being written, and
// demanding the landlord's residency before the tenant's name has been typed is
// how a form gets abandoned. Activate is where they become mandatory.
func (d Draft) Validate() error {
	switch {
	case d.TenantID == "":
		return fmt.Errorf("%w: no organisation", ErrDraft)
	case d.Property == "" || d.Unit == "":
		return fmt.Errorf("%w: a lease must name the unit it lets", ErrDraft)
	case !d.Term.Valid():
		return fmt.Errorf("%w: no term, so it bills nothing and expires never", ErrDraft)
	case d.NoticeDays < 0:
		return fmt.Errorf("%w: a notice period of %d days", ErrDraft, d.NoticeDays)
	}
	if err := d.Terms.Validate(); err != nil {
		return err
	}

	var tenants int
	seen := map[string]bool{}
	for _, p := range d.Parties {
		if err := p.Validate(); err != nil {
			return err
		}
		if p.PartyID != "" {
			if seen[p.PartyID] {
				return fmt.Errorf("%w: %s is named twice", ErrDraft, p.Name)
			}
			seen[p.PartyID] = true
		}
		if p.Role == RoleTenant {
			tenants++
		}
	}
	if tenants == 0 {
		return fmt.Errorf("%w: no tenant — somebody has to be liable for the rent", ErrDraft)
	}

	if !d.LockInUntil.Zero() && !d.Term.To().Zero() && d.Term.To().Before(d.LockInUntil) {
		return fmt.Errorf("%w: a lock-in to %s on a tenancy ending %s locks the tenant in past the "+
			"end of their own lease", ErrDraft, d.LockInUntil, d.Term.To())
	}
	return nil
}

// Create turns a validated draft into a lease in draft state, and the event it
// publishes.
//
// The lease starts in draft however complete the draft is. Activation is a
// separate, checked step (Activate), because that is where the no-double-let
// constraint and the TDS gate apply and where an event with consequences is
// published.
func (d Draft) Create() (Lease, Terms, Event, error) {
	if err := d.Validate(); err != nil {
		return Lease{}, Terms{}, "", err
	}
	l := Lease{
		TenantID: d.TenantID, Property: d.Property, Unit: d.Unit,
		State: StateDraft, Term: d.Term,
		NoticeDays: d.NoticeDays, LockInUntil: d.LockInUntil,
		Renews: d.Renews, Tax: d.Tax,
	}
	if err := l.Validate(); err != nil {
		return Lease{}, Terms{}, "", err
	}
	return l, d.Terms, EventCreated, nil
}

// DepositCharge is what the deposit costs the tenant at the start, separately
// from rent.
//
// It is not a Period: a deposit is not rent, it is not income when received, and
// ADR-0006 posts it to deposit_liability rather than to rent. Returning it as a
// distinct thing is what stops it being generated as a first month at double
// rent, which is the error every rental product makes once.
type DepositCharge struct {
	AmountMinor int64
	DueOn       effective.Date
	HeldBy      DepositHolder
}

// Deposit returns the deposit charge, and false where there is none.
func (l Lease) Deposit(t Terms) (DepositCharge, bool) {
	if t.DepositMinor <= 0 {
		return DepositCharge{}, false
	}
	return DepositCharge{
		AmountMinor: t.DepositMinor,
		DueOn:       l.Term.From(),
		HeldBy:      t.DepositHeldBy,
	}, true
}
