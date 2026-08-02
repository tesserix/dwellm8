package service

import (
	"context"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
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
	OwnerOnboarding = store.OwnerOnboarding
	OwnerOnboarded  = store.OwnerOnboarded
	Profile         = store.Profile
)

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
