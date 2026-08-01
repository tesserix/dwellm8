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
)

// Grant is what one URL permits: one document, for one eSign transaction,
// until one moment. Nothing in it is derivable from a lease id or a tenant id
// — the signature is what makes the path, and the key never leaves the server.
type Grant struct {
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
	case g.DocumentRef == "" || strings.Contains(g.DocumentRef, "|"):
		return "", fmt.Errorf("docurl: not a usable document ref")
	case g.TxnID == "" || strings.Contains(g.TxnID, "|"):
		return "", fmt.Errorf("docurl: not a usable transaction id")
	case g.ExpiresAt.IsZero():
		return "", fmt.Errorf("docurl: a grant expires, always")
	}
	payload := base64.RawURLEncoding.EncodeToString(
		fmt.Appendf(nil, "%s|%s|%d", g.DocumentRef, g.TxnID, g.ExpiresAt.Unix()))
	return payload + "." + s.sign(payload), nil
}

// Parse verifies a token and returns its grant. Constant-time on the
// signature, and expiry is checked here rather than left to the caller — a
// caller cannot forget a check that already happened.
func (s *Signer) Parse(token string, now time.Time) (Grant, error) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || !hmac.Equal([]byte(s.sign(payload)), []byte(sig)) {
		return Grant{}, ErrURL
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Grant{}, ErrURL
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return Grant{}, ErrURL
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || now.Unix() >= exp {
		return Grant{}, ErrURL
	}
	return Grant{DocumentRef: parts[0], TxnID: parts[1], ExpiresAt: time.Unix(exp, 0)}, nil
}

func (s *Signer) sign(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
