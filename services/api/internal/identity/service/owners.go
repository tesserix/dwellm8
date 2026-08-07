package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Owners is the identity module's seam for manager-led owner onboarding and
// self-served profiles. #240.
type Owners struct {
	principals *store.Principals
	log        *slog.Logger
}

// NewOwners builds the service.
func NewOwners(p *store.Principals, log *slog.Logger) *Owners {
	return &Owners{principals: p, log: log}
}

// Re-exported request/response shapes.
type (
	OwnerOnboarding  = store.OwnerOnboarding
	OwnerOnboarded   = store.OwnerOnboarded
	Profile          = store.Profile
	ManagedPortfolio = store.ManagedPortfolio
)

// Portfolios lists the live mandates a firm holds, named.
func (o *Owners) Portfolios(ctx context.Context, firmOrgID string) ([]ManagedPortfolio, error) {
	return o.principals.PortfoliosFor(ctx, firmOrgID)
}

// Portfolio is the firm's mandate over one owner. store.ErrNoMandate when the
// firm does not act for them.
func (o *Owners) Portfolio(ctx context.Context, firmOrgID, ownerOrgID string) (ManagedPortfolio, error) {
	return o.principals.PortfolioFor(ctx, firmOrgID, ownerOrgID)
}

// OwnerParty is whose books an owner organisation is — the party rent is
// credited to (#302).
func (o *Owners) OwnerParty(ctx context.Context, ownerOrgID string) (string, error) {
	return o.principals.OwnerParty(ctx, ownerOrgID)
}

// PreOnboard reserves the owner's identity, organisation and the firm's
// mandate. Idempotent — a second property joins the existing organisation.
func (o *Owners) PreOnboard(ctx context.Context, req OwnerOnboarding) (OwnerOnboarded, error) {
	out, err := o.principals.PreOnboardOwner(ctx, req)
	if err != nil {
		return OwnerOnboarded{}, err
	}
	o.log.Info("owner pre-onboarded",
		"owner_org", out.OrgID, "party", out.PartyID, "created", out.CreatedOrg, "grant", out.GrantID)
	return out, nil
}

// ProfileByParty reads a party's self-presented profile.
func (o *Owners) ProfileByParty(ctx context.Context, partyID string) (Profile, error) {
	return o.principals.ProfileByParty(ctx, partyID)
}

// UpdateProfileByParty fills in self-served PI. The phone never moves here.
func (o *Owners) UpdateProfileByParty(ctx context.Context, partyID, displayName, email string) (Profile, error) {
	return o.principals.UpdateProfileByParty(ctx, partyID, displayName, email)
}

// RecordTaxProfile writes what a landlord has furnished, into the books the
// rent is credited to rather than the firm's — a mandate lets a manager record
// this, it does not move whose income it is.
func (o *Owners) RecordTaxProfile(ctx context.Context, partyID string, p store.TaxProfile) error {
	org, err := o.principals.OwnerOrgOf(ctx, partyID)
	if err != nil {
		return err
	}
	return o.principals.SaveTaxProfile(ctx, tenancy.ID(org), partyID, p)
}

// TaxProfile reads what was furnished as of a date, which is not always what is
// furnished now.
func (o *Owners) TaxProfile(ctx context.Context, partyID string, on time.Time) (store.TaxProfile, error) {
	org, err := o.principals.OwnerOrgOf(ctx, partyID)
	if err != nil {
		return store.TaxProfile{}, err
	}
	return o.principals.TaxProfile(ctx, tenancy.ID(org), partyID, on)
}
