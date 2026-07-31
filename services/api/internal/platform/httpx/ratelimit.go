package httpx

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Rate limiting. Issue #228, and the denial-of-service row of the threat model.
//
// Two limits, because they fail differently and neither substitutes for the
// other:
//
//   - **Per tenant**, so one organisation cannot take the service down for the
//     rest. This is the one that matters on a normal day.
//   - **Per route, unkeyed**, for what is reachable *without* a tenant — the
//     webhook endpoint, where an attacker has no organisation to be limited by.
//     Signature verification is cheap but not free, and an unverified flood still
//     costs a request and a decision per delivery.
//
// # Why a token bucket rather than a fixed window
//
// A fixed window lets an attacker send the whole allowance in the last
// millisecond of one window and again in the first of the next — twice the limit
// in an instant, which is exactly the shape that hurts. A bucket refills
// continuously, so the burst is bounded by the bucket and the sustained rate by
// the refill.
//
// # Why in-process
//
// A shared limiter in Redis is the correct answer for a fleet, and it is a
// dependency in the request path that fails at exactly the moment load is high.
// This is per replica, which means the effective limit is the configured one
// times the replica count — stated here rather than discovered, and the number to
// set is the per-replica one. When there is a reason to make it exact, Redis is
// where it goes.

// Limit is a rate: Burst requests immediately, refilling at Every.
type Limit struct {
	// Burst is how many requests can arrive at once.
	Burst int
	// Every is how long one token takes to come back.
	Every time.Duration
}

// bucket is one key's allowance, refilled lazily on read rather than by a ticker
// — a ticker per tenant is a goroutine per tenant, and the arithmetic is the same.
type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter hands out allowances per key.
//
// The zero Limiter is unusable; construct with NewLimiter. Safe for concurrent
// use, which is the whole point of it.
type Limiter struct {
	limit Limit
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	// seen is when the map was last swept. Buckets are dropped once they have
	// been full and idle for a while, so a limiter keyed by tenant does not grow
	// by one entry per organisation that ever made a request.
	swept time.Time
}

// NewLimiter builds a limiter. now is injectable so the tests can move time
// rather than sleep through it — a rate limiter tested with sleeps is a test
// suite that takes minutes and still races.
func NewLimiter(l Limit, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	if l.Burst < 1 {
		l.Burst = 1
	}
	if l.Every <= 0 {
		l.Every = time.Second
	}
	return &Limiter{limit: l, now: now, buckets: map[string]*bucket{}, swept: now()}
}

// Allow takes a token for a key, and reports whether there was one — plus how
// long until the next token, for the Retry-After header.
//
// The wait is returned even when the request is allowed (as zero), so a caller
// cannot forget to set the header on the path that matters.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(l.limit.Burst), last: now}
		l.buckets[key] = b
	}

	// Refill for the time that has passed, capped at the burst.
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens += float64(elapsed) / float64(l.limit.Every)
		if b.tokens > float64(l.limit.Burst) {
			b.tokens = float64(l.limit.Burst)
		}
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	// How long until one whole token exists.
	missing := 1 - b.tokens
	return false, time.Duration(missing * float64(l.limit.Every))
}

// sweepLocked drops buckets that are full and idle. Called on the request path
// because a background goroutine for this is a lifecycle to get wrong.
func (l *Limiter) sweepLocked(now time.Time) {
	const every = time.Minute
	if now.Sub(l.swept) < every {
		return
	}
	l.swept = now
	idle := 10 * time.Duration(l.limit.Burst) * l.limit.Every
	if idle < time.Minute {
		idle = time.Minute
	}
	for k, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, k)
		}
	}
}

// KeyFunc decides what a request is limited by. Returning "" means the request
// is not limited by this middleware at all — which is a decision the caller
// makes explicitly, not a default.
type KeyFunc func(*http.Request) string

// Limited wraps a handler with a limiter.
//
// A shed request answers **429 with Retry-After**, deliberately and not 503: a
// payment provider retries a 429 and treats some other codes as delivered. Getting
// that wrong on the webhook route turns a rate limit into lost deliveries, which
// is a worse outcome than the load it was shedding.
func Limited(l *Limiter, key KeyFunc, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := key(r)
		if k == "" {
			next.ServeHTTP(w, r)
			return
		}
		allowed, wait := l.Allow(k)
		if allowed {
			next.ServeHTTP(w, r)
			return
		}
		seconds := int(wait/time.Second) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":       "too many requests",
			"retry_after": seconds,
		})
	})
}

// ByTenant limits per organisation, from the header the middleware resolves the
// tenant from. Requests with no tenant are not limited by this — they are the
// public and webhook surfaces, and ByRoute is what covers them.
func ByTenant(header string) KeyFunc {
	return func(r *http.Request) string { return r.Header.Get(header) }
}

// ByRoute limits everything hitting a route, with no key at all.
//
// For the webhook endpoint, where the caller is unauthenticated by definition and
// there is no tenant to key by: an attacker cannot be limited by an identity they
// do not have to present.
func ByRoute(name string) KeyFunc {
	return func(*http.Request) string { return name }
}

// Size is how many buckets are held. Exposed for the test that asserts idle ones
// are dropped — a limiter keyed by tenant that never forgets is a slow leak with
// a plausible excuse.
func (l *Limiter) Size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
