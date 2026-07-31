package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/httpx"
)

// A clock the test moves, rather than sleeps it waits through. A rate limiter
// tested with real time is a suite that takes minutes and still races.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func newClock() *clock { return &clock{at: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)} }

// The burst is what arrives at once; the refill is what comes back over time.
func TestABucketAllowsItsBurstAndThenRefills(t *testing.T) {
	c := newClock()
	l := httpx.NewLimiter(httpx.Limit{Burst: 3, Every: time.Second}, c.now)

	for i := range 3 {
		if ok, _ := l.Allow("org-1"); !ok {
			t.Fatalf("request %d of the burst was shed", i+1)
		}
	}
	ok, wait := l.Allow("org-1")
	if ok {
		t.Fatal("a fourth request went through a burst of three")
	}
	if wait <= 0 || wait > time.Second {
		t.Errorf("the wait is %s, want somewhere inside the second it takes to refill", wait)
	}

	// Half a second buys nothing; a whole one buys exactly one.
	c.advance(500 * time.Millisecond)
	if ok, _ := l.Allow("org-1"); ok {
		t.Error("half a refill period produced a whole token")
	}
	c.advance(500 * time.Millisecond)
	if ok, _ := l.Allow("org-1"); !ok {
		t.Error("a full refill period produced no token")
	}

	// And the bucket does not fill past its burst however long it idles.
	c.advance(time.Hour)
	for i := range 3 {
		if ok, _ := l.Allow("org-1"); !ok {
			t.Fatalf("after an hour idle, request %d was shed", i+1)
		}
	}
	if ok, _ := l.Allow("org-1"); ok {
		t.Error("an hour of idling banked more than the burst — the limit is a rate, not a savings account")
	}
}

// The point of keying by tenant: one organisation's flood is its own.
func TestOneOrganisationCannotShedAnother(t *testing.T) {
	c := newClock()
	l := httpx.NewLimiter(httpx.Limit{Burst: 2, Every: time.Second}, c.now)

	for range 2 {
		l.Allow("org-1")
	}
	if ok, _ := l.Allow("org-1"); ok {
		t.Fatal("org-1 is not limited")
	}
	if ok, _ := l.Allow("org-2"); !ok {
		t.Error("org-2 was shed because org-1 was noisy — that is the outage this exists to prevent")
	}
}

// A shed request answers 429 with Retry-After, not 503. A payment provider
// retries a 429; getting the code wrong on the webhook route turns a rate limit
// into lost deliveries.
func TestAShedRequestSaysWhenToComeBack(t *testing.T) {
	c := newClock()
	l := httpx.NewLimiter(httpx.Limit{Burst: 1, Every: 2 * time.Second}, c.now)

	var served int
	h := httpx.Limited(l, httpx.ByRoute("webhook"), http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			served++
			w.WriteHeader(http.StatusOK)
		}))

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/webhooks/cashfree", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("the first request answered %d", first.Code)
	}

	shed := httptest.NewRecorder()
	h.ServeHTTP(shed, httptest.NewRequest(http.MethodPost, "/webhooks/cashfree", nil))
	if shed.Code != http.StatusTooManyRequests {
		t.Fatalf("a shed request answered %d, want 429 — a provider retries a 429 and may treat "+
			"other codes as delivered", shed.Code)
	}
	retry := shed.Header().Get("Retry-After")
	if retry == "" {
		t.Fatal("a shed request does not say when to come back")
	}
	if n, err := strconv.Atoi(retry); err != nil || n < 1 {
		t.Errorf("Retry-After is %q, which is not a number of seconds a client can use", retry)
	}
	if served != 1 {
		t.Errorf("the handler ran %d times — a shed request must not reach it, which is the whole "+
			"point of shedding it", served)
	}
}

// The webhook route is limited with no key, because an attacker has no
// organisation to be limited by.
func TestAnUnauthenticatedRouteIsLimitedWithoutATenant(t *testing.T) {
	c := newClock()
	l := httpx.NewLimiter(httpx.Limit{Burst: 2, Every: time.Second}, c.now)
	h := httpx.Limited(l, httpx.ByRoute("webhook"), http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	codes := make([]int, 0, 3)
	for range 3 {
		rec := httptest.NewRecorder()
		// No tenant header at all: this is what an attacker sends.
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/cashfree", nil))
		codes = append(codes, rec.Code)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Errorf("an unauthenticated flood was not shed: %v", codes)
	}
}

// A request the key function declines to key is not limited — and that is a
// decision the caller makes rather than a default that silently exempts things.
func TestAnUnkeyedRequestPassesThrough(t *testing.T) {
	c := newClock()
	l := httpx.NewLimiter(httpx.Limit{Burst: 1, Every: time.Hour}, c.now)
	h := httpx.Limited(l, httpx.ByTenant("X-Org"), http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	for i := range 5 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d with no tenant header was shed by a per-tenant limit", i+1)
		}
	}

	// With a tenant, the same limiter bites.
	req := httptest.NewRequest(http.MethodGet, "/leases", nil)
	req.Header.Set("X-Org", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal("the first tenanted request was shed")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("the second tenanted request answered %d", rec.Code)
	}
}

// Concurrent callers must not get more than the burst between them. Run with
// -race, which is how CI runs it.
func TestTheBurstHoldsUnderConcurrency(t *testing.T) {
	c := newClock()
	l := httpx.NewLimiter(httpx.Limit{Burst: 50, Every: time.Minute}, c.now)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Allow("org-1"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed != 50 {
		t.Errorf("%d of 200 concurrent requests were allowed against a burst of 50", allowed)
	}
}

// A limiter keyed by tenant must not grow by one entry for every organisation
// that has ever made a request.
func TestIdleBucketsAreDropped(t *testing.T) {
	c := newClock()
	l := httpx.NewLimiter(httpx.Limit{Burst: 2, Every: time.Second}, c.now)

	for i := range 100 {
		l.Allow("org-" + strconv.Itoa(i))
	}
	if n := l.Size(); n != 100 {
		t.Fatalf("%d buckets after 100 organisations, want 100", n)
	}

	// Long enough that every bucket is full and idle, and past the sweep interval.
	c.advance(time.Hour)
	l.Allow("org-fresh")
	if n := l.Size(); n > 2 {
		t.Errorf("%d buckets after an hour of idleness, want the sweep to have dropped them", n)
	}
}
