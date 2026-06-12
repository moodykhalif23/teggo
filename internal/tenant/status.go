package tenant

import (
	"context"
	"sync"
	"time"

	"b2bcommerce/internal/store/gen"
)

// StatusCache answers "what is this org's lifecycle status?" cheaply enough to
// sit on every authenticated request. Entries expire after ttl so a suspension
// bites within seconds across all API nodes; Invalidate makes it immediate on
// the node that performed the change.
type StatusCache struct {
	q   *gen.Queries
	ttl time.Duration

	mu      sync.Mutex
	entries map[int64]statusEntry
}

type statusEntry struct {
	status string
	until  time.Time
}

func NewStatusCache(q *gen.Queries, ttl time.Duration) *StatusCache {
	return &StatusCache{q: q, ttl: ttl, entries: make(map[int64]statusEntry)}
}

// Status returns the org's status and true, or ("", false) when it can't be
// determined (org missing, transient DB error) — callers fail OPEN on false: a
// nonexistent org holds no data, and a DB blip must not take the API down.
func (c *StatusCache) Status(ctx context.Context, orgID int64) (string, bool) {
	c.mu.Lock()
	if e, ok := c.entries[orgID]; ok && time.Now().Before(e.until) {
		c.mu.Unlock()
		return e.status, true
	}
	c.mu.Unlock()

	org, err := c.q.GetOrganization(ctx, orgID)
	if err != nil {
		return "", false
	}
	c.mu.Lock()
	c.entries[orgID] = statusEntry{status: org.Status, until: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return org.Status, true
}

// Invalidate drops the cached entry so the next request re-reads the database.
func (c *StatusCache) Invalidate(orgID int64) {
	c.mu.Lock()
	delete(c.entries, orgID)
	c.mu.Unlock()
}
