package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrToken is any refusal to accept a token. Deliberately one error: a caller
// gets 401 for all of them, and a message that distinguished "expired" from
// "wrong audience" would be a probe oracle.
var ErrToken = errors.New("auth: the token was not accepted")

// Keys supplies Google's current token-signing certificates, by key id.
//
// An interface so the tests can sign with a key they generated. A verifier that
// can only be tested against Google's live endpoint is one nobody tests.
type Keys interface {
	Key(ctx context.Context, kid string) (*rsa.PublicKey, error)
}

// Verifier checks GIP ID tokens for one project.
type Verifier struct {
	// ProjectID is the GCP project. It is both the issuer's suffix and the
	// audience, and checking it is what stops a token minted for another
	// Google project being accepted here.
	ProjectID string
	// TenantPrefix names this product's pools — "dwellm8", giving dwellm8-own
	// and the rest.
	TenantPrefix string
	Keys         Keys
	// Now is injectable for the expiry tests. nil means time.Now.
	Now func() time.Time
	// Leeway forgives small clock differences between Google and this process.
	// Small on purpose: a generous leeway extends the life of a stolen token.
	Leeway time.Duration
}

type header struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

type claims struct {
	Iss      string `json:"iss"`
	Aud      string `json:"aud"`
	Sub      string `json:"sub"`
	Exp      int64  `json:"exp"`
	Iat      int64  `json:"iat"`
	AuthTime int64  `json:"auth_time"`
	Email    string `json:"email"`
	Phone    string `json:"phone_number"`
	Firebase struct {
		Tenant         string `json:"tenant"`
		SignInProvider string `json:"sign_in_provider"`
	} `json:"firebase"`
}

// Verify checks a raw bearer token and returns who it is for.
//
// The checks are in the order that refuses fastest and leaks least, and every
// one of them is a way JWT verification is got wrong somewhere:
//
//  1. **The algorithm is fixed at RS256**, and the header's `alg` is compared
//     to it rather than used to select a verifier. Trusting the header is how
//     `alg: none` and HMAC-with-the-public-key both work.
//  2. **The key is chosen by `kid` from Google's set**, so a token signed with a
//     key we have never seen is refused rather than searched for.
//  3. **Issuer and audience are both checked** against the project. A token from
//     another Google project is a valid Google token and is not ours.
//  4. **Expiry, issue time and auth time** are checked, with a small leeway.
//  5. **The tenant claim decides the surface**, and its absence means staff —
//     which is why an unrecognised tenant is an error rather than a fallback.
func (v *Verifier) Verify(ctx context.Context, raw string) (Principal, error) {
	if v.ProjectID == "" || v.TenantPrefix == "" || v.Keys == nil {
		return Principal{}, errors.New("auth: the verifier is not configured")
	}

	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Principal{}, fmt.Errorf("%w: not a JWT", ErrToken)
	}

	var h header
	if err := decodeSegment(parts[0], &h); err != nil {
		return Principal{}, fmt.Errorf("%w: unreadable header", ErrToken)
	}
	// Fixed, not selected. The header is the attacker's to write.
	if h.Alg != "RS256" {
		return Principal{}, fmt.Errorf("%w: %q is not the signing algorithm", ErrToken, h.Alg)
	}
	if h.Kid == "" {
		return Principal{}, fmt.Errorf("%w: no key id", ErrToken)
	}

	key, err := v.Keys.Key(ctx, h.Kid)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: no such signing key", ErrToken)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Principal{}, fmt.Errorf("%w: unreadable signature", ErrToken)
	}
	signed := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, signed[:], sig); err != nil {
		return Principal{}, fmt.Errorf("%w: the signature does not verify", ErrToken)
	}

	var c claims
	if err := decodeSegment(parts[1], &c); err != nil {
		return Principal{}, fmt.Errorf("%w: unreadable claims", ErrToken)
	}
	if err := v.checkClaims(c); err != nil {
		return Principal{}, err
	}

	p := Principal{
		UID: c.Sub, Phone: c.Phone, Email: c.Email,
		SignInProvider: c.Firebase.SignInProvider,
	}
	if c.Firebase.Tenant == "" {
		// No tenant means the project level, which only Dwellm8 staff reach. A
		// surface's sign-in always produces a tenant, so this cannot be forged by
		// a customer without a project-level credential.
		p.Staff = true
		return p, nil
	}
	surface, err := SurfaceFor(c.Firebase.Tenant, v.TenantPrefix)
	if err != nil {
		return Principal{}, err
	}
	p.Surface = surface
	return p, nil
}

func (v *Verifier) checkClaims(c claims) error {
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	leeway := v.Leeway

	switch {
	case c.Aud != v.ProjectID:
		return fmt.Errorf("%w: minted for another audience", ErrToken)
	case c.Iss != "https://securetoken.google.com/"+v.ProjectID:
		return fmt.Errorf("%w: minted by another issuer", ErrToken)
	case c.Sub == "":
		return fmt.Errorf("%w: no subject", ErrToken)
	case c.Exp == 0 || now.After(time.Unix(c.Exp, 0).Add(leeway)):
		return fmt.Errorf("%w: expired", ErrToken)
	case c.Iat == 0 || now.Add(leeway).Before(time.Unix(c.Iat, 0)):
		return fmt.Errorf("%w: issued in the future", ErrToken)
	case c.AuthTime != 0 && now.Add(leeway).Before(time.Unix(c.AuthTime, 0)):
		return fmt.Errorf("%w: authenticated in the future", ErrToken)
	}
	return nil
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
