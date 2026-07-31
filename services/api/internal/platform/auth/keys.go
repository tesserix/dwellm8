package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GoogleCertsURL publishes the certificates GIP signs ID tokens with.
const GoogleCertsURL = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"

// GoogleKeys fetches and caches Google's signing certificates.
//
// Cached because a fetch per request would put an outbound HTTPS call in the
// authentication path of every request — and refreshed because Google rotates
// the keys, so a cache with no expiry authenticates nobody a day after it warms.
//
// The refresh is driven by Cache-Control on the response rather than by a
// constant of ours: Google states how long the set is good for, and inventing a
// shorter interval is extra load while inventing a longer one is an outage on
// rotation day.
type GoogleKeys struct {
	URL    string
	Client *http.Client

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
	// Now is injectable for the cache tests.
	Now func() time.Time
}

// NewGoogleKeys builds a key source with sensible transport limits. The timeout
// matters: this call sits in front of every authenticated request, so a slow
// Google is a slow API rather than a hung one.
func NewGoogleKeys() *GoogleKeys {
	return &GoogleKeys{
		URL:    GoogleCertsURL,
		Client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Key returns the public key for a key id, fetching the set if the cache is
// cold or stale.
func (g *GoogleKeys) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}

	g.mu.RLock()
	key, ok := g.keys[kid]
	fresh := now().Before(g.expires)
	g.mu.RUnlock()
	if ok && fresh {
		return key, nil
	}

	if err := g.refresh(ctx, now()); err != nil {
		// A stale key is better than no key: if Google is unreachable and the
		// cache has the kid, an expired cache should not take authentication down
		// with it. The token's own expiry still bounds how long this can help.
		g.mu.RLock()
		defer g.mu.RUnlock()
		if key, ok := g.keys[kid]; ok {
			return key, nil
		}
		return nil, err
	}

	g.mu.RLock()
	defer g.mu.RUnlock()
	if key, ok := g.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("auth: no signing key %q", kid)
}

func (g *GoogleKeys) refresh(ctx context.Context, now time.Time) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.URL, nil)
	if err != nil {
		return err
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: fetching signing keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: fetching signing keys: %s", resp.Status)
	}

	var certs map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&certs); err != nil {
		return fmt.Errorf("auth: reading signing keys: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(certs))
	for kid, certPEM := range certs {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			continue
		}
		keys[kid] = pub
	}
	if len(keys) == 0 {
		return fmt.Errorf("auth: the signing key set was empty")
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.keys = keys
	g.expires = now.Add(maxAge(resp.Header.Get("Cache-Control")))
	return nil
}

// maxAge reads Google's own freshness, falling back to an hour. Never zero: a
// zero would refetch the set on every request and turn a cache into a proxy.
func maxAge(cacheControl string) time.Duration {
	const fallback = time.Hour
	for _, part := range strings.Split(cacheControl, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "max-age=") {
			continue
		}
		secs, err := strconv.Atoi(strings.TrimPrefix(part, "max-age="))
		if err != nil || secs <= 0 {
			return fallback
		}
		return time.Duration(secs) * time.Second
	}
	return fallback
}
