package auth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
)

// Every check in the verifier is a way JWT verification is got wrong somewhere,
// so every one of them is a test that forges the token that would exploit it.

const project = "tesseracthub-480811"

type keyring struct {
	kid string
	pub *rsa.PublicKey
	err error
}

func (k *keyring) Key(_ context.Context, kid string) (*rsa.PublicKey, error) {
	if k.err != nil {
		return nil, k.err
	}
	if kid != k.kid {
		return nil, fmt.Errorf("no key %q", kid)
	}
	return k.pub, nil
}

type mint struct {
	priv *rsa.PrivateKey
	kid  string
}

func newMint(t *testing.T) (*mint, *keyring) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return &mint{priv: key, kid: "k1"}, &keyring{kid: "k1", pub: &key.PublicKey}
}

// token builds a signed JWT. header and claims are maps so a test can forge any
// shape, including ones a real minter would never produce.
func (m *mint) token(t *testing.T, head, body map[string]any) string {
	t.Helper()
	if head == nil {
		head = map[string]any{"alg": "RS256", "kid": m.kid, "typ": "JWT"}
	}
	seg := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshalling: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := seg(head) + "." + seg(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, m.priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func goodClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss":          "https://securetoken.google.com/" + project,
		"aud":          project,
		"sub":          "uid-123",
		"iat":          now.Add(-time.Minute).Unix(),
		"exp":          now.Add(time.Hour).Unix(),
		"phone_number": "+919876543210",
		"firebase": map[string]any{
			"tenant":           "dwellm8-live",
			"sign_in_provider": "phone",
		},
	}
}

func verifier(k auth.Keys, now time.Time) *auth.Verifier {
	return &auth.Verifier{
		ProjectID: project, TenantPrefix: "dwellm8", Keys: k,
		Now: func() time.Time { return now }, Leeway: 30 * time.Second,
	}
}

// A tenant signing into the tenant app, which is the ordinary case.
func TestAValidTokenNamesItsSurfaceAndItsPerson(t *testing.T) {
	m, ring := newMint(t)
	now := time.Now()

	p, err := verifier(ring, now).Verify(context.Background(), m.token(t, nil, goodClaims(now)))
	if err != nil {
		t.Fatalf("a good token was refused: %v", err)
	}
	if p.Surface != auth.SurfaceLive {
		t.Errorf("the token resolved to surface %q, want live", p.Surface)
	}
	if p.Staff {
		t.Error("a tenant was treated as staff")
	}
	if p.UID != "uid-123" || p.Phone != "+919876543210" || p.SignInProvider != "phone" {
		t.Errorf("the principal is %+v", p)
	}
	// A uid is unique within its pool and not across pools, so the key carries
	// the surface. Two people with the same uid in different pools are two people.
	if p.Key() != "live:uid-123" {
		t.Errorf("the principal key is %q", p.Key())
	}
}

// The forgeries. Each of these is a real technique.
func TestTheForgeriesAreRefused(t *testing.T) {
	m, ring := newMint(t)
	now := time.Now()
	v := verifier(ring, now)

	// A second key, to sign tokens this verifier should not accept.
	other, _ := newMint(t)

	for _, c := range []struct {
		name  string
		token func() string
	}{
		{"alg none, which is the oldest trick there is", func() string {
			// Unsigned, and claiming it needs no signature. The verifier compares
			// alg to RS256 rather than using it to choose, so this dies at the
			// comparison.
			return m.token(t, map[string]any{"alg": "none", "kid": m.kid}, goodClaims(now))
		}},
		{"HS256, hoping the public key is used as an HMAC secret", func() string {
			return m.token(t, map[string]any{"alg": "HS256", "kid": m.kid}, goodClaims(now))
		}},
		{"signed by a key we have never seen", func() string {
			return other.token(t, map[string]any{"alg": "RS256", "kid": "k1"}, goodClaims(now))
		}},
		{"a key id nobody published", func() string {
			return m.token(t, map[string]any{"alg": "RS256", "kid": "attacker"}, goodClaims(now))
		}},
		{"a valid Google token for another project", func() string {
			c := goodClaims(now)
			c["aud"] = "some-other-project"
			c["iss"] = "https://securetoken.google.com/some-other-project"
			return m.token(t, nil, c)
		}},
		{"the right audience and the wrong issuer", func() string {
			c := goodClaims(now)
			c["iss"] = "https://evil.example.com/" + project
			return m.token(t, nil, c)
		}},
		{"expired an hour ago", func() string {
			c := goodClaims(now)
			c["exp"] = now.Add(-time.Hour).Unix()
			return m.token(t, nil, c)
		}},
		{"issued tomorrow", func() string {
			c := goodClaims(now)
			c["iat"] = now.Add(24 * time.Hour).Unix()
			return m.token(t, nil, c)
		}},
		{"nobody in the subject", func() string {
			c := goodClaims(now)
			c["sub"] = ""
			return m.token(t, nil, c)
		}},
		{"a tenant that is not one of ours", func() string {
			c := goodClaims(now)
			c["firebase"] = map[string]any{"tenant": "someone-else-prod", "sign_in_provider": "phone"}
			return m.token(t, nil, c)
		}},
		{"a Dwellm8-looking tenant that does not exist", func() string {
			c := goodClaims(now)
			c["firebase"] = map[string]any{"tenant": "dwellm8-superuser", "sign_in_provider": "phone"}
			return m.token(t, nil, c)
		}},
		{"not a JWT at all", func() string { return "hello" }},
		{"a tampered payload", func() string {
			tok := m.token(t, nil, goodClaims(now))
			parts := strings.Split(tok, ".")
			forged := goodClaims(now)
			forged["sub"] = "somebody-else"
			b, _ := json.Marshal(forged)
			parts[1] = base64.RawURLEncoding.EncodeToString(b)
			return strings.Join(parts, ".")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, err := v.Verify(context.Background(), c.token())
			if !errors.Is(err, auth.ErrToken) {
				t.Fatalf("accepted: %+v (err %v)", p, err)
			}
			// Every refusal is the same error, so a caller cannot use the message
			// to learn which check it failed.
			if !strings.Contains(err.Error(), "the token was not accepted") {
				t.Errorf("the refusal distinguishes itself: %v", err)
			}
		})
	}
}

// The product-owner exception: a token with no tenant is project level, which
// only Dwellm8 staff can obtain. A customer's sign-in always produces a tenant.
func TestATokenWithNoTenantIsStaff(t *testing.T) {
	m, ring := newMint(t)
	now := time.Now()

	c := goodClaims(now)
	delete(c, "phone_number")
	c["email"] = "sam@dwellm8.com"
	c["firebase"] = map[string]any{"sign_in_provider": "google.com"}

	p, err := verifier(ring, now).Verify(context.Background(), m.token(t, nil, c))
	if err != nil {
		t.Fatalf("a staff token was refused: %v", err)
	}
	if !p.Staff {
		t.Fatal("a project-level token was not recognised as staff")
	}
	if p.Surface != "" {
		t.Errorf("staff resolved to surface %q — staff are outside every surface", p.Surface)
	}
	if p.Key() != "staff:uid-123" {
		t.Errorf("the staff key is %q", p.Key())
	}
}

// Each surface maps to exactly one pool, and the mapping is derived rather than
// configured so that two surfaces cannot be pointed at one pool by editing a
// values file.
func TestEverySurfaceHasItsOwnPool(t *testing.T) {
	seen := map[string]auth.Surface{}
	for _, s := range auth.Surfaces() {
		id := s.TenantID("dwellm8")
		if other, clash := seen[id]; clash {
			t.Errorf("%s and %s share the pool %q — that merges two user bases", s, other, id)
		}
		seen[id] = s

		back, err := auth.SurfaceFor(id, "dwellm8")
		if err != nil || back != s {
			t.Errorf("%q resolved back to %q (%v)", id, back, err)
		}
	}
	if len(seen) != 6 {
		t.Errorf("%d pools for 6 surfaces", len(seen))
	}

	// Another product's pool in the same GCP project is not ours.
	if _, err := auth.SurfaceFor("mark8ly-storefront", "dwellm8"); !errors.Is(err, auth.ErrToken) {
		t.Error("another product's user pool was accepted as one of ours")
	}
}

// A token that expired inside the leeway is still refused once the leeway has
// passed — the window is small on purpose, because a generous one extends the
// life of a stolen token.
func TestTheLeewayIsSmallAndFinite(t *testing.T) {
	m, ring := newMint(t)
	now := time.Now()
	tok := m.token(t, nil, goodClaims(now))

	justExpired := verifier(ring, now.Add(time.Hour+10*time.Second))
	if _, err := justExpired.Verify(context.Background(), tok); err != nil {
		t.Errorf("a token ten seconds past expiry was refused inside a thirty-second leeway: %v", err)
	}

	wellExpired := verifier(ring, now.Add(time.Hour+time.Minute))
	if _, err := wellExpired.Verify(context.Background(), tok); !errors.Is(err, auth.ErrToken) {
		t.Error("a token a minute past expiry was accepted")
	}
}
