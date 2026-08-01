package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
)

// GoogleKeys caches Google's signing certificates and refreshes them on
// Google's own schedule, not ours. Every case here is a way the cache could
// authenticate against a stale or absent key.

func certPEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	var buf strings.Builder
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("encoding certificate: %v", err)
	}
	return buf.String()
}

func newKeypair(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return key
}

func jwksServer(t *testing.T, cacheControl string, kids ...string) (*httptest.Server, map[string]*rsa.PrivateKey) {
	t.Helper()
	keys := make(map[string]*rsa.PrivateKey, len(kids))
	certs := make(map[string]string, len(kids))
	for _, kid := range kids {
		k := newKeypair(t)
		keys[kid] = k
		certs[kid] = certPEM(t, k)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cacheControl != "" {
			w.Header().Set("Cache-Control", cacheControl)
		}
		_ = json.NewEncoder(w).Encode(certs)
	}))
	t.Cleanup(srv.Close)
	return srv, keys
}

// The plain case: a cold cache fetches, and the key it returns verifies
// against the certificate the server published.
func TestAColdCacheFetchesAndReturnsTheKey(t *testing.T) {
	srv, keys := jwksServer(t, "max-age=3600", "kid-1")
	g := &auth.GoogleKeys{URL: srv.URL, Client: srv.Client()}

	got, err := g.Key(context.Background(), "kid-1")
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if got.N.Cmp(keys["kid-1"].PublicKey.N) != 0 {
		t.Fatal("the returned key does not match the one the server published")
	}
}

// A second call inside the cache's freshness window must not hit the server
// again — that outbound call sits in front of every authenticated request.
func TestAFreshCacheIsNotRefetched(t *testing.T) {
	var hits int32
	srv, _ := jwksServerCounting(t, "max-age=3600", &hits, "kid-1")
	g := &auth.GoogleKeys{URL: srv.URL, Client: srv.Client()}

	for i := 0; i < 3; i++ {
		if _, err := g.Key(context.Background(), "kid-1"); err != nil {
			t.Fatalf("Key: %v", err)
		}
	}
	if hits != 1 {
		t.Fatalf("fetched %d times for three lookups inside the cache window", hits)
	}
}

func jwksServerCounting(t *testing.T, cacheControl string, hits *int32, kids ...string) (*httptest.Server, map[string]*rsa.PrivateKey) {
	t.Helper()
	keys := make(map[string]*rsa.PrivateKey, len(kids))
	certs := make(map[string]string, len(kids))
	for _, kid := range kids {
		k := newKeypair(t)
		keys[kid] = k
		certs[kid] = certPEM(t, k)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if cacheControl != "" {
			w.Header().Set("Cache-Control", cacheControl)
		}
		_ = json.NewEncoder(w).Encode(certs)
	}))
	t.Cleanup(srv.Close)
	return srv, keys
}

// Past the cache's expiry, a lookup refetches — Google rotates keys, and a
// cache with no expiry authenticates nobody a day after it warms.
func TestAnExpiredCacheRefetches(t *testing.T) {
	var hits int32
	srv, keys := jwksServerCounting(t, "max-age=1", &hits, "kid-1")
	now := time.Now()
	g := &auth.GoogleKeys{URL: srv.URL, Client: srv.Client(), Now: func() time.Time { return now }}

	if _, err := g.Key(context.Background(), "kid-1"); err != nil {
		t.Fatalf("first Key: %v", err)
	}
	now = now.Add(2 * time.Second)
	if _, err := g.Key(context.Background(), "kid-1"); err != nil {
		t.Fatalf("second Key: %v", err)
	}
	if hits != 2 {
		t.Fatalf("fetched %d times across the expiry, want 2", hits)
	}
	_ = keys
}

// Missing or unparseable max-age falls back to an hour rather than to zero —
// zero would turn the cache into a proxy, refetching on every request.
func TestMissingMaxAgeFallsBackToAnHourRatherThanRefetchingEveryTime(t *testing.T) {
	var hits int32
	srv, _ := jwksServerCounting(t, "", &hits, "kid-1")
	now := time.Now()
	g := &auth.GoogleKeys{URL: srv.URL, Client: srv.Client(), Now: func() time.Time { return now }}

	if _, err := g.Key(context.Background(), "kid-1"); err != nil {
		t.Fatalf("first Key: %v", err)
	}
	now = now.Add(30 * time.Minute)
	if _, err := g.Key(context.Background(), "kid-1"); err != nil {
		t.Fatalf("second Key: %v", err)
	}
	if hits != 1 {
		t.Fatalf("fetched %d times inside the one-hour fallback window", hits)
	}
}

// A stale key is better than no key: if Google is unreachable and the cache
// still holds the kid, an expired cache must not take authentication down
// with it.
func TestAStaleKeyIsServedWhenTheRefetchFails(t *testing.T) {
	srv, keys := jwksServer(t, "max-age=1", "kid-1")
	now := time.Now()
	g := &auth.GoogleKeys{URL: srv.URL, Client: srv.Client(), Now: func() time.Time { return now }}

	if _, err := g.Key(context.Background(), "kid-1"); err != nil {
		t.Fatalf("warming the cache: %v", err)
	}
	srv.Close() // Google is now unreachable
	now = now.Add(2 * time.Second)

	got, err := g.Key(context.Background(), "kid-1")
	if err != nil {
		t.Fatalf("Key with an unreachable server and a stale cache: %v", err)
	}
	if got.N.Cmp(keys["kid-1"].PublicKey.N) != 0 {
		t.Fatal("the stale key served does not match what was cached")
	}
}

// A kid the refresh cannot find is refused, not silently accepted.
func TestAnUnknownKidIsRefused(t *testing.T) {
	srv, _ := jwksServer(t, "max-age=3600", "kid-1")
	g := &auth.GoogleKeys{URL: srv.URL, Client: srv.Client()}

	if _, err := g.Key(context.Background(), "kid-does-not-exist"); err == nil {
		t.Fatal("expected an error for an unknown key id")
	}
}

// An empty published set is an error, not a cache that silently authenticates
// nobody.
func TestAnEmptyKeySetIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	t.Cleanup(srv.Close)
	g := &auth.GoogleKeys{URL: srv.URL, Client: srv.Client()}

	if _, err := g.Key(context.Background(), "kid-1"); err == nil {
		t.Fatal("expected an error for an empty key set")
	}
}

// A server that never comes up at all, with nothing cached, is an error too —
// not a nil key a caller forgets to check.
func TestAnUnreachableServerWithNothingCachedIsAnError(t *testing.T) {
	g := &auth.GoogleKeys{URL: "http://127.0.0.1:1", Client: &http.Client{Timeout: 200 * time.Millisecond}}
	if _, err := g.Key(context.Background(), "kid-1"); err == nil {
		t.Fatal("expected an error when the server cannot be reached and the cache is cold")
	}
}

// Concurrent lookups against a cold cache must not race on the map: this
// runs under -race in CI.
func TestConcurrentLookupsDoNotRace(t *testing.T) {
	srv, _ := jwksServer(t, "max-age=3600", "kid-1", "kid-2")
	g := &auth.GoogleKeys{URL: srv.URL, Client: srv.Client()}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			kid := "kid-1"
			if i%2 == 0 {
				kid = "kid-2"
			}
			if _, err := g.Key(context.Background(), kid); err != nil {
				t.Errorf("concurrent Key(%s): %v", kid, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestNewGoogleKeysPointsAtGoogleWithABoundedTimeout(t *testing.T) {
	g := auth.NewGoogleKeys()
	if g.URL != auth.GoogleCertsURL {
		t.Fatalf("URL = %q, want the published certs endpoint", g.URL)
	}
	if g.Client == nil || g.Client.Timeout <= 0 || g.Client.Timeout > 30*time.Second {
		t.Fatalf("Client timeout = %v, want a short bounded timeout", g.Client.Timeout)
	}
}
