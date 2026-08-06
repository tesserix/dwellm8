package service

import (
	"context"
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/identity/domain/registration"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// RegistrationRows is what this service needs of the store, sized to it.
type RegistrationRows interface {
	ManagerProfile(ctx context.Context, firm tenancy.ID) (store.ManagerProfile, error)
	SaveManagerProfile(ctx context.Context, firm tenancy.ID, p store.ManagerProfile) error
	AddRegistration(ctx context.Context, firm tenancy.ID, r store.Registration) error
	LiveRegistrations(ctx context.Context, firm tenancy.ID, on time.Time) ([]store.Registration, error)
	RecordDocument(ctx context.Context, firm tenancy.ID, d store.ManagerDocument) error
	HeldDocuments(ctx context.Context, firm tenancy.ID) (map[registration.Kind]registration.Held, error)
}

// Registrations is the identity module's public seam for what a managing firm
// must hold before it may take somebody else's property on (#282).
type Registrations struct{ principals RegistrationRows }

func NewRegistrations(p RegistrationRows) *Registrations { return &Registrations{principals: p} }

// Re-exported shapes, so a surface names these without importing the store.
type (
	FirmProfile = store.ManagerProfile
	Authority   = store.Registration
	Document    = store.ManagerDocument
)

// Standing is the whole answer one screen needs: what the firm has said, what
// it holds, and what is still missing — in one read, because a checklist that
// costs a round trip per line is a checklist nobody finishes.
type Standing struct {
	Profile       FirmProfile
	Registrations []Authority
	Required      []registration.Requirement
	Outstanding   []registration.Requirement
	Fields        []registration.Field
	// MayManage is the question every other screen asks: a firm with an
	// outstanding checklist or no live registration may not take a mandate.
	MayManage bool
}

func (s *Registrations) Standing(ctx context.Context, firm tenancy.ID, on time.Time) (Standing, error) {
	profile, err := s.principals.ManagerProfile(ctx, firm)
	if err != nil && err != store.ErrNoProfile {
		return Standing{}, err
	}
	// No profile is a legitimate state — a firm that has just been named — and
	// the checklist for one is every line of it.
	constitution := registration.Constitution(profile.Constitution)
	if constitution == "" {
		constitution = registration.Proprietorship
	}

	held, err := s.principals.HeldDocuments(ctx, firm)
	if err != nil {
		return Standing{}, err
	}
	live, err := s.principals.LiveRegistrations(ctx, firm, on)
	if err != nil {
		return Standing{}, err
	}

	out := Standing{
		Profile:       profile,
		Registrations: live,
		Required:      registration.RequiredDocuments(constitution),
		Outstanding:   registration.Outstanding(constitution, held, on),
		Fields:        registration.RequiredFields(constitution),
	}
	out.MayManage = profile.LegalName != "" && len(out.Outstanding) == 0 && len(live) > 0
	return out, nil
}

// Save writes the firm's details.
func (s *Registrations) Save(ctx context.Context, firm tenancy.ID, p store.ManagerProfile) error {
	if p.LegalName == "" {
		return fmt.Errorf("identity: the firm's legal name is what everything else is registered against")
	}
	return s.principals.SaveManagerProfile(ctx, firm, p)
}

// File records one state's agent registration.
func (s *Registrations) File(ctx context.Context, firm tenancy.ID, r store.Registration) error {
	return s.principals.AddRegistration(ctx, firm, r)
}

// Record notes an uploaded document.
func (s *Registrations) Record(ctx context.Context, firm tenancy.ID, d store.ManagerDocument) error {
	return s.principals.RecordDocument(ctx, firm, d)
}
