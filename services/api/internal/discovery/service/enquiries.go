package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

// ErrNotConvertible is a conversion attempted before the owner has engaged:
// an unanswered enquiry is one side, not a tenancy.
var ErrNotConvertible = errors.New("enquiry: respond before converting")

// MaskedCalls opens a proxy connection between two verified parties, neither
// of whose numbers this module holds (#138). The provider is handed two of its
// own references and answers with a proxy number and an expiry.
type MaskedCalls interface {
	Open(ctx context.Context, ownerRef, prospectRef string) (provider, providerRef, proxyMasked string, expires time.Time, err error)
}

// TenancyDrafter turns an accepted applicant into a lease draft with
// everything carried over (#142). The lease module satisfies it.
type TenancyDrafter interface {
	DraftTenancy(ctx context.Context, a TenancyApplication) (leaseID string, err error)
}

// TenancyApplication is the carry-over, in this module's terms.
type TenancyApplication struct {
	PropertyID   string
	UnitID       string
	Start        effective.Date
	End          effective.Date
	RentMinor    int64
	DepositMinor int64
	TenantName   string
	TenantPhone  string
	TenantEmail  string
}

// Enquiries runs the pipeline from a prospect's first message to a tenancy.
type Enquiries struct {
	store     *store.Enquiries
	listings  *store.Listings
	prospects *store.Prospects
	bridges   MaskedCalls
	drafter   TenancyDrafter
	log       *slog.Logger
}

// NewEnquiries wires the service.
func NewEnquiries(s *store.Enquiries, l *store.Listings, p *store.Prospects, log *slog.Logger) *Enquiries {
	return &Enquiries{store: s, listings: l, prospects: p, log: log}
}

// WithBridges teaches responding to open a masked connection. Optional: nil
// means the response is recorded and the connection waits for a provider.
func (s *Enquiries) WithBridges(b MaskedCalls) *Enquiries {
	s.bridges = b
	return s
}

// WithDrafter teaches conversion to draft the tenancy. Optional in wiring,
// required in fact — Convert refuses without it.
func (s *Enquiries) WithDrafter(d TenancyDrafter) *Enquiries {
	s.drafter = d
	return s
}

// Enquire records a verified prospect's enquiry or inspection request against
// a live listing. The prospect side of the funnel: the caller holds a token,
// not a session.
func (s *Enquiries) Enquire(ctx context.Context, token, listingID, kind, message string,
	scheduledFor *time.Time) (store.Enquiry, error) {
	p, err := s.resolveToken(ctx, token)
	if err != nil {
		return store.Enquiry{}, err
	}
	if !p.Verified {
		return store.Enquiry{}, store.ErrNotVerified
	}
	switch kind {
	case "enquiry", "inspection", "callback":
	default:
		return store.Enquiry{}, fmt.Errorf("%w: kind must be enquiry, inspection or callback",
			store.ErrNoEnquiry)
	}
	e, err := s.store.Create(ctx, listingID, p.ID, kind, message, scheduledFor)
	if err != nil {
		return store.Enquiry{}, err
	}
	s.log.Info("enquiry received", "enquiry", e.ID, "listing", listingID, "kind", kind)
	return e, nil
}

// Timeline is the prospect's own history.
func (s *Enquiries) Timeline(ctx context.Context, token string) ([]store.Enquiry, error) {
	p, err := s.resolveToken(ctx, token)
	if err != nil {
		return nil, err
	}
	return s.store.ForProspect(ctx, p.ID)
}

// Pipeline is the owner's queue, with each enquirer's masked contact attached
// — the only form of the number that exists to attach.
func (s *Enquiries) Pipeline(ctx context.Context, state string) ([]store.Enquiry, error) {
	list, err := s.store.ForOwner(ctx, state)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.ProspectID)
	}
	masked, err := s.prospects.MaskedContacts(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].ContactMasked = masked[list[i].ProspectID]
	}
	return list, nil
}

// Respond records the owner's first engagement and, when a provider is wired,
// opens the masked bridge in the same act — both sides have now engaged, which
// is the trigger's precondition.
func (s *Enquiries) Respond(ctx context.Context, id string, actor events.Actor) (store.Bridge, error) {
	e, err := s.store.Get(ctx, id)
	if err != nil {
		return store.Bridge{}, err
	}
	if e.State == "new" {
		if err := s.store.Move(ctx, id, "owner_responded", actor); err != nil {
			return store.Bridge{}, err
		}
	}
	if s.bridges == nil {
		return store.Bridge{}, nil
	}

	masked, err := s.prospects.MaskedContacts(ctx, []string{e.ProspectID})
	if err != nil {
		return store.Bridge{}, err
	}
	provider, ref, proxy, expires, err := s.bridges.Open(ctx, "owner:"+e.TenantID, masked[e.ProspectID])
	if err != nil {
		return store.Bridge{}, fmt.Errorf("opening the bridge: %w", err)
	}
	b := store.Bridge{EnquiryID: id, Provider: provider, ProviderRef: ref,
		ProxyMasked: proxy, ExpiresAt: expires}
	if b.ID, err = s.store.OpenBridge(ctx, b); err != nil {
		return store.Bridge{}, err
	}
	s.log.Info("contact bridge opened", "enquiry", id, "expires", expires)
	return b, nil
}

// Advance moves an enquiry along the pipeline: scheduled, completed, closed,
// spam.
func (s *Enquiries) Advance(ctx context.Context, id, state string, actor events.Actor) error {
	return s.store.Move(ctx, id, state, actor)
}

// Conversion is what the manager confirms at conversion time. Name and phone
// are supplied here because this module deliberately holds neither — the
// prospect's number lives with the masked-calling provider.
type Conversion struct {
	Start       effective.Date
	End         effective.Date
	TenantName  string
	TenantPhone string
	TenantEmail string
	// RentMinor overrides the advertised rent when the parties negotiated;
	// zero means the listing's rent carries over unchanged.
	RentMinor int64
}

// Convert turns an engaged enquiry into a lease draft with the unit, rent,
// deposit and dates carried over (#142), pauses the listing — off the market
// while the paperwork runs — and publishes the fact. The listing moves to let,
// automatically, when the tenancy actually starts.
func (s *Enquiries) Convert(ctx context.Context, enquiryID string, c Conversion,
	actor events.Actor) (leaseID string, err error) {
	if s.drafter == nil {
		return "", errors.New("enquiry: conversion is not wired to the lease module")
	}
	e, err := s.store.Get(ctx, enquiryID)
	if err != nil {
		return "", err
	}
	if e.State == "new" || e.State == "spam" || e.State == "closed" {
		return "", fmt.Errorf("%w: the enquiry is %s", ErrNotConvertible, e.State)
	}
	l, err := s.listings.Get(ctx, e.ListingID)
	if err != nil {
		return "", err
	}

	rent := l.Costs.RentMinor
	if c.RentMinor > 0 {
		rent = c.RentMinor
	}
	leaseID, err = s.drafter.DraftTenancy(ctx, TenancyApplication{
		PropertyID: l.PropertyID, UnitID: l.UnitID,
		Start: c.Start, End: c.End,
		RentMinor: rent, DepositMinor: l.Costs.DepositMinor,
		TenantName: c.TenantName, TenantPhone: c.TenantPhone, TenantEmail: c.TenantEmail,
	})
	if err != nil {
		return "", err
	}

	// Off the market, not let: the tenancy is a draft and can still fall
	// through, and a paused listing resumes in one call if it does.
	if l.State == domain.StateLive {
		if err := s.listings.Move(ctx, e.ListingID, domain.StatePaused, actor); err != nil {
			s.log.Error("pausing the listing after conversion", "listing", e.ListingID, "error", err)
		}
	}
	s.log.Info("enquiry converted", "enquiry", enquiryID, "lease", leaseID, "listing", e.ListingID)
	return leaseID, nil
}

func (s *Enquiries) resolveToken(ctx context.Context, token string) (store.Prospect, error) {
	p, err := s.prospects.ByToken(ctx, hash(token))
	if errors.Is(err, store.ErrNoProspect) {
		return store.Prospect{}, ErrBadToken
	}
	return p, err
}

// DevBridge fabricates a proxy number. Dev only, the DevVerifier's contract.
type DevBridge struct{ Log *slog.Logger }

// Open pretends a provider allocated a proxy.
func (b DevBridge) Open(ctx context.Context, ownerRef, prospectRef string) (string, string, string, time.Time, error) {
	b.Log.Warn("dev bridge: fabricating a proxy number")
	return "dev", "dev-bridge", "XXXXXX0000", time.Now().Add(72 * time.Hour), nil
}
