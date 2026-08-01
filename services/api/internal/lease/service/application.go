package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Application is a tenancy application arriving from another module — the
// discovery funnel's prospect-to-tenancy conversion (dwellm8#142) — stated in
// plain terms so the caller never reaches this module's domain (ADR-0001 §3).
type Application struct {
	PropertyID string
	UnitID     string

	Start effective.Date
	// End is zero for an open-ended tenancy.
	End        effective.Date
	NoticeDays int

	RentMinor    int64
	DepositMinor int64
	DueDay       int

	// The applicant becomes the lease's tenant party. PartyID is optional —
	// the residents resolver identifies a party by phone, which is how every
	// renter acquires an identity (ADR-0029 §2).
	TenantPartyID string
	TenantName    string
	TenantPhone   string
	TenantEmail   string
}

// DraftFromApplication writes a lease in draft carrying everything the
// applicant already gave, so nobody types it a third time. Draft, not active:
// accepting an applicant and starting a tenancy stay two acts, and the
// no-double-let guard fires at the second.
func (s *Leases) DraftFromApplication(ctx context.Context, a Application) (Created, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return Created{}, tenancy.ErrNoTenant
	}

	var (
		term effective.Interval
		err  error
	)
	if a.End.Zero() {
		term, err = effective.Since(a.Start)
	} else {
		term, err = effective.Between(a.Start, a.End)
	}
	if err != nil {
		return Created{}, fmt.Errorf("%w: %v", domain.ErrDraft, err)
	}

	if a.NoticeDays == 0 {
		a.NoticeDays = 30
	}
	if a.DueDay == 0 {
		a.DueDay = 5
	}

	d := domain.Draft{
		TenantID: tenant.String(),
		Property: a.PropertyID, Unit: a.UnitID,
		Term: term, NoticeDays: a.NoticeDays,
		Terms: domain.Terms{
			RentMinor: a.RentMinor, Cycle: domain.Monthly,
			DueDay: domain.DueDay(a.DueDay), DepositMinor: a.DepositMinor,
			DepositHeldBy: domain.HeldByOwner,
		},
		Parties: []domain.Party{{
			PartyID: a.TenantPartyID, Role: domain.RoleTenant,
			Name: a.TenantName, Phone: a.TenantPhone, Email: a.TenantEmail,
		}},
	}
	return s.Create(ctx, d)
}

// ActiveOnUnit reports the tenancy occupying a unit on a date, "" when it is
// free. The discovery module asks this before advertising: a listing must not
// describe a flat somebody lives in past its availability date (#135).
func (s *Leases) ActiveOnUnit(ctx context.Context, unitID string, on effective.Date) (string, error) {
	id, err := s.store.ActiveOnUnit(ctx, unitID, on)
	if errors.Is(err, tenancy.ErrNoTenant) {
		return "", err
	}
	return id, err
}
