package authz

import (
	"sync"
	"time"
)

// Cache holds check decisions for a bounded window.
//
// The contract: a decision — allow or deny alike — may be honoured for at most
// TTL after the relationship changed. TTL is therefore the revocation bound
// dwellm8#150 asks for, and it must stay small enough that "I revoked the
// mandate and they got one more request in" is an acceptable sentence. There
// is no invalidation call on purpose: an invalidation the tuple pipeline must
// remember to send is an invalidation that will one day not be sent.
type Cache struct {
	TTL time.Duration
	Max int

	mu sync.Mutex
	m  map[string]entry
}

type entry struct {
	allowed bool
	expires time.Time
}

func NewCache(ttl time.Duration, max int) *Cache {
	return &Cache{TTL: ttl, Max: max, m: make(map[string]entry)}
}

func (c *Cache) Get(user, relation, object string) (allowed, ok bool) {
	if c == nil {
		return false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[user+"|"+relation+"|"+object]
	if !ok || time.Now().After(e.expires) {
		return false, false
	}
	return e.allowed, true
}

func (c *Cache) Put(user, relation, object string, allowed bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Full means shed, not grow: dropping a random-ish entry re-asks OpenFGA,
	// which is the safe direction to be wrong in.
	if len(c.m) >= c.Max {
		for k := range c.m {
			delete(c.m, k)
			break
		}
	}
	c.m[user+"|"+relation+"|"+object] = entry{allowed: allowed, expires: time.Now().Add(c.TTL)}
}
