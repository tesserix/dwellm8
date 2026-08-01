// Package docurl issues and verifies the signed, short-lived URLs an eSign
// provider is given as docUrl. dwellm8#212: the lease PDF must be fetchable
// from the public internet for the signing window, and a lease is personal
// data — so the URL is unguessable, single-purpose, expiring, revocable, and
// every fetch of it is logged.
package docurl

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Grant is what one URL permits: one document, for one eSign transaction,
// until one moment. Nothing in it is derivable from a lease id or a tenant id
// — the signature is what makes the path, and the key never leaves the server.
//
// Org is carried in the payload because the fetch arrives with no sign-in: it
// is what lets the handler open a tenant-scoped transaction for the revocation
// check and the access log. Readable in the token, like the rest of the
// payload — the signature is the secret, not the ids.
type Grant struct {
	Org         tenancy.ID
	DocumentRef string
	TxnID       string
	ExpiresAt   time.Time
}

// ErrURL is every refusal. One error on purpose: expired, tampered, revoked
// and never-issued must be indistinguishable to the holder, or the URL becomes
// an oracle for probing what exists (the same rule as the token verifier,
// ADR-0027 §4).
var ErrURL = errors.New("docurl: this link is not usable")

// Signer mints and verifies grants with an HMAC key.
type Signer struct{ key []byte }

func NewSigner(key string) (*Signer, error) {
	if len(key) < 32 {
		return nil, errors.New("docurl: the signing key is too short to mean anything")
	}
	return &Signer{key: []byte(key)}, nil
}

// Token encodes a grant as <payload>.<signature>, both base64url. The payload
// is readable — there is nothing secret in it — and the signature is what
// makes it unforgeable and non-enumerable.
func (s *Signer) Token(g Grant) (string, error) {
	switch {
	case g.Org == "" || strings.Contains(g.Org.String(), "|"):
		return "", fmt.Errorf("docurl: not a usable organisation id")
	case g.DocumentRef == "" || strings.Contains(g.DocumentRef, "|"):
		return "", fmt.Errorf("docurl: not a usable document ref")
	case g.TxnID == "" || strings.Contains(g.TxnID, "|"):
		return "", fmt.Errorf("docurl: not a usable transaction id")
	case g.ExpiresAt.IsZero():
		return "", fmt.Errorf("docurl: a grant expires, always")
	}
	payload := base64.RawURLEncoding.EncodeToString(
		fmt.Appendf(nil, "%s|%s|%s|%d", g.Org, g.DocumentRef, g.TxnID, g.ExpiresAt.Unix()))
	return payload + "." + s.sign(payload), nil
}

// Parse verifies a token and returns its grant. Constant-time on the
// signature, and expiry is checked here rather than left to the caller — a
// caller cannot forget a check that already happened.
func (s *Signer) Parse(token string, now time.Time) (Grant, error) {
	g, ok := s.decode(token)
	if !ok || now.Unix() >= g.ExpiresAt.Unix() {
		return Grant{}, ErrURL
	}
	return g, nil
}

// Attribution returns the grant a token names when its signature is genuine,
// live or not. It exists for the access log alone — an expired fetch is still
// an attempt worth recording against its transaction — and never for serving:
// the serving path goes through Parse, where expiry refuses. A tampered or
// forged token has no attribution and is recorded by nobody.
func (s *Signer) Attribution(token string) (Grant, bool) {
	return s.decode(token)
}

func (s *Signer) decode(token string) (Grant, bool) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || !hmac.Equal([]byte(s.sign(payload)), []byte(sig)) {
		return Grant{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Grant{}, false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 4 {
		return Grant{}, false
	}
	exp, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return Grant{}, false
	}
	return Grant{
		Org:         tenancy.ID(parts[0]),
		DocumentRef: parts[1],
		TxnID:       parts[2],
		ExpiresAt:   time.Unix(exp, 0),
	}, true
}

func (s *Signer) sign(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
