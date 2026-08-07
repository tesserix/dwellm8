package service

import (
	"context"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

// Tenancy is the lease as the person living in it sees it.
type Tenancy = store.Tenancy

// Tenancy reads one tenancy's summary as of a date.
//
// The caller says which lease; whether it is theirs is settled by the session
// (ADR-0029) and by the policy underneath, not by an argument passed here. A
// lease they are not on reads as ErrNoLease, which is also what a lease that
// does not exist reads as.
func (s *Leases) Tenancy(ctx context.Context, leaseID string, on effective.Date) (Tenancy, error) {
	if leaseID == "" {
		return Tenancy{}, store.ErrNoLease
	}
	return s.store.Tenancy(ctx, leaseID, on)
}

// ServeNotice records notice served on a live tenancy and returns its new
// summary. #239. The domain refuses the illegal cases: wrong actor, wrong
// state, a move-out inside the notice period or outside the term.
func (s *Leases) ServeNotice(ctx context.Context, leaseID string, n domain.Notice) (Tenancy, error) {
	if leaseID == "" {
		return Tenancy{}, store.ErrNoLease
	}
	if _, err := s.store.ServeNotice(ctx, leaseID, n); err != nil {
		return Tenancy{}, err
	}
	return s.store.Tenancy(ctx, leaseID, n.ServedOn)
}

// ActiveOnProperty reads the property's current tenancy, if it has one. The
// bool reports whether it does — a vacant property is a normal answer, not
// store.ErrNoLease.
func (s *Leases) ActiveOnProperty(ctx context.Context, propertyID string, on effective.Date) (Tenancy, bool, error) {
	return s.store.ActiveOnProperty(ctx, propertyID, on)
}

// Residents is the identity module's seam, as this module needs it: a verified
// mobile number, and the party it identifies. ADR-0001 §3 — a service
// interface, never identity's store.
//
// It exists because of when a renter acquires an identity. The landlord types
// their tenant's number into a lease weeks before that person opens the app, and
// unless the party id is settled at that moment the sign-in has nothing to
// attach to and the tenancy is unreachable by the one person it is about.
type Residents interface {
	PartyFor(ctx context.Context, phone string) (string, error)
}

// WithResidents returns a copy that resolves a party id for any tenant or
// guarantor named by number rather than by id.
//
// A copy rather than a setter, so the wiring in main() reads as one expression
// and a service that was built without identity cannot acquire it later from
// somewhere less visible.
func (s *Leases) WithResidents(r Residents) *Leases {
	out := *s
	out.residents = r
	return &out
}

// resolveParties fills in the party id of anybody named only by mobile number.
//
// Only tenants and guarantors: an occupant may be a child with no phone, is
// liable for nothing, and has no sign-in to attach. A party that already carries
// an id is left alone — a landlord adding a second flat for an existing tenant
// names them by id, and re-resolving would be a lookup that can only agree.
func (s *Leases) resolveParties(ctx context.Context, d domain.Draft) (domain.Draft, error) {
	if s.residents == nil {
		return d, nil
	}
	parties := make([]domain.Party, len(d.Parties))
	copy(parties, d.Parties)

	for i, p := range parties {
		if p.PartyID != "" || p.Phone == "" || p.Role == domain.RoleOccupant {
			continue
		}
		id, err := s.residents.PartyFor(ctx, p.Phone)
		if err != nil {
			return domain.Draft{}, fmt.Errorf("lease: identifying %s: %w", p.Name, err)
		}
		parties[i].PartyID = id
	}
	d.Parties = parties
	return d, nil
}

// Expiring is a tenancy running out. See store.Expiring.
type Expiring = store.Expiring

// Expiring lists tenancies ending within `within` days, for the renewal reminder
// and the owner's dashboard. ADR-0010 §6's view, read through the seam.
func (s *Leases) Expiring(ctx context.Context, within int) ([]Expiring, error) {
	return s.store.Expiring(ctx, within)
}

// Due is the next rent a tenancy owes. See store.Due.
type Due = store.Due

// Upcoming lists the next rent falling due on every live tenancy between two
// dates, for the manager's reminder list (#337).
func (s *Leases) Upcoming(ctx context.Context, from, through effective.Date, limit int) ([]Due, error) {
	return s.store.Upcoming(ctx, from, through, limit)
}

// Live lists the tenancies that are running — active or in notice — which is what
// an automation walks when it has something to say about every tenancy.
func (s *Leases) Live(ctx context.Context, limit int) ([]domain.Lease, error) {
	return s.store.Billable(ctx, limit)
}

// Roster is the whole live book: see store.Roster.
type Roster = store.Roster

// Roster counts every live tenancy and the rent in force, in one query rather
// than by paging Live (#306).
func (s *Leases) Roster(ctx context.Context, on effective.Date) (Roster, error) {
	return s.store.Roster(ctx, on)
}

// PartiesOf returns the tenant and the owner on a tenancy as at a date.
func (s *Leases) PartiesOf(ctx context.Context, leaseID string, on effective.Date) (tenant, owner string, err error) {
	return s.store.Parties(ctx, leaseID, on)
}

// TaxPath is the section that governs a payment on a date, with the facts it was
// selected from. ErrNoTaxFacts where the tenancy has none in force then.
//
// The surface composing a deduction needs both: the section decides which rate
// row and which threshold, and the facts are what the manager is shown when the
// answer is questioned.
func (s *Leases) TaxPath(ctx context.Context, leaseID string, on effective.Date) (tds.Path, tds.Facts, error) {
	facts, err := s.store.TaxFacts(ctx, leaseID, on)
	if err != nil {
		return tds.Path{}, tds.Facts{}, err
	}
	path, err := tds.Select(facts)
	if err != nil {
		return tds.Path{}, facts, err
	}
	return path, facts, nil
}

// AnnualRentToOwner is the year's rent to one landlord across every tenancy of
// theirs — the figure section 194-I's threshold is tested against, aggregated to
// the payee rather than to the property.
func (s *Leases) AnnualRentToOwner(ctx context.Context, ownerParty string, on effective.Date) (int64, error) {
	return s.store.AnnualRentToOwner(ctx, ownerParty, on)
}

// ErrNoTaxFacts is re-exported so a surface matches one error.
var ErrNoTaxFacts = store.ErrNoTaxFacts
