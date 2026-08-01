package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
)

// ErrBadToken is a token that is not one of ours. One refusal whatever the
// cause — unknown, malformed, expired — for the verifier's reason: a
// distinguishable error is an oracle.
var ErrBadToken = errors.New("prospect: unrecognised token")

// ErrBadPhone is a phone number an OTP cannot be sent to.
var ErrBadPhone = errors.New("prospect: phone must be an Indian mobile, as +91XXXXXXXXXX")

// PhoneVerifier sends and confirms the OTP that turns a browsing stranger into
// somebody an owner can be connected to (#137's verification point). Confirm
// returns the masked-calling provider's reference and the masked display form
// — never the number, which this module refuses to hold.
type PhoneVerifier interface {
	Start(ctx context.Context, phone string) error
	Confirm(ctx context.Context, phone, code string) (contactRef, contactMasked string, err error)
}

// Prospects issues browsing tokens and runs the verification point.
type Prospects struct {
	store    *store.Prospects
	verifier PhoneVerifier
	log      *slog.Logger
}

// NewProspects wires the service.
func NewProspects(s *store.Prospects, v PhoneVerifier, log *slog.Logger) *Prospects {
	return &Prospects{store: s, verifier: v, log: log}
}

var phonePattern = regexp.MustCompile(`^\+91[6-9][0-9]{9}$`)

// Issue mints a browsing token and its prospect record. The raw token goes to
// the browser and nowhere else; the database holds its hash, so a copy of the
// database is not a copy of anybody's browsing key.
func (s *Prospects) Issue(ctx context.Context) (token string, err error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("minting a token: %w", err)
	}
	token = "dwp_" + hex.EncodeToString(b)
	if _, err := s.store.Ensure(ctx, hash(token)); err != nil {
		return "", err
	}
	return token, nil
}

// Resolve returns the prospect a token names.
func (s *Prospects) Resolve(ctx context.Context, token string) (store.Prospect, error) {
	p, err := s.store.ByToken(ctx, hash(token))
	if errors.Is(err, store.ErrNoProspect) {
		return store.Prospect{}, ErrBadToken
	}
	return p, err
}

// ErrNoVerifier is verification attempted with no OTP provider wired — a
// deployment state, answered per request rather than by refusing to boot,
// because search and shortlisting still work without it.
var ErrNoVerifier = errors.New("prospect: phone verification is not configured")

// StartVerification sends the OTP.
func (s *Prospects) StartVerification(ctx context.Context, token, phone string) error {
	if s.verifier == nil {
		return ErrNoVerifier
	}
	if !phonePattern.MatchString(phone) {
		return ErrBadPhone
	}
	if _, err := s.Resolve(ctx, token); err != nil {
		return err
	}
	return s.verifier.Start(ctx, phone)
}

// ConfirmVerification checks the OTP and records the verification package.
// The phone number passes through to the provider and is gone: what lands in
// the database is the provider's reference and the masked form.
func (s *Prospects) ConfirmVerification(ctx context.Context, token, phone, code string) error {
	if s.verifier == nil {
		return ErrNoVerifier
	}
	if !phonePattern.MatchString(phone) {
		return ErrBadPhone
	}
	if _, err := s.Resolve(ctx, token); err != nil {
		return err
	}
	ref, masked, err := s.verifier.Confirm(ctx, phone, code)
	if err != nil {
		return err
	}
	if err := s.store.Verify(ctx, hash(token), ref, masked); err != nil {
		return err
	}
	s.log.Info("prospect verified", "contact", masked)
	return nil
}

// ShortlistAdd saves a listing for the token's prospect.
func (s *Prospects) ShortlistAdd(ctx context.Context, token, listingID string) error {
	p, err := s.Resolve(ctx, token)
	if err != nil {
		return err
	}
	return s.store.ShortlistAdd(ctx, p.ID, listingID)
}

// ShortlistRemove takes one off.
func (s *Prospects) ShortlistRemove(ctx context.Context, token, listingID string) error {
	p, err := s.Resolve(ctx, token)
	if err != nil {
		return err
	}
	return s.store.ShortlistRemove(ctx, p.ID, listingID)
}

// Shortlist lists the prospect's saved listings, honestly — a saved flat that
// has been let says so.
func (s *Prospects) Shortlist(ctx context.Context, token string) ([]store.ShortlistItem, error) {
	p, err := s.Resolve(ctx, token)
	if err != nil {
		return nil, err
	}
	return s.store.Shortlist(ctx, p.ID)
}

func hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// DevVerifier accepts the fixed code 000000 and fabricates a provider
// reference. Dev only — config.validate refuses it anywhere else, the same
// contract as impersonation: a verifier that forgot to verify must not work in
// production for everybody.
type DevVerifier struct{ Log *slog.Logger }

// Start pretends to send an OTP.
func (v DevVerifier) Start(ctx context.Context, phone string) error {
	v.Log.Warn("dev verifier: pretending to send an OTP", "code", "000000")
	return nil
}

// Confirm accepts 000000. The reference is a hash of the number rather than
// the number, so even the dev path never persists a phone.
func (v DevVerifier) Confirm(ctx context.Context, phone, code string) (string, string, error) {
	if code != "000000" {
		return "", "", errors.New("prospect: the code did not match")
	}
	sum := sha256.Sum256([]byte(phone))
	return "dev-" + hex.EncodeToString(sum[:6]), "XXXXXX" + phone[len(phone)-4:], nil
}
