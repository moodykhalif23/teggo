// Package billing implements platform plans, feature flags and usage metering
// (SAAS.md #2). Every org holds one plan; plans enable premium features
// (subscriptions, rebates, fx, merchandising, assistant) and cap metered usage
// (orders + ai_calls per month, storage_bytes lifetime). Enforcement happens in
// the Gate middleware (one path-rule table) and at order-creation call sites.
// An org WITHOUT a plan row is intentionally unmetered — provisioning always
// assigns one, so that state only describes the platform org in legacy DBs.
package billing

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"b2bcommerce/internal/store/gen"
)

// Premium feature keys (plan.features entries).
const (
	FeatureSubscriptions = "subscriptions"
	FeatureRebates       = "rebates"
	FeatureFX            = "fx"
	FeatureMerchandising = "merchandising"
	FeatureAssistant     = "assistant"
)

// Metered usage keys (plan.limits entries / usage_counters.metric).
const (
	MetricOrders  = "orders"
	MetricAICalls = "ai_calls"
	MetricStorage = "storage_bytes"
)

// PeriodKeyFor returns the counter period for a metric: monthly buckets for
// flow metrics, the lifetime bucket for gauges (storage).
func PeriodKeyFor(metric string, at time.Time) string {
	if metric == MetricStorage {
		return "all"
	}
	return at.UTC().Format("2006-01")
}

// Entitlements is an org's resolved plan: which features it may use and what
// its caps are.
type Entitlements struct {
	PlanCode string
	PlanName string
	Status   string
	Features map[string]bool
	Limits   map[string]int64
}

// Allows reports whether a premium feature is enabled. A nil receiver (no plan
// row — unmetered org) allows everything.
func (e *Entitlements) Allows(feature string) bool {
	if e == nil {
		return true
	}
	return e.Features[feature]
}

// Limit returns the cap for a metric; ok=false means unlimited.
func (e *Entitlements) Limit(metric string) (int64, bool) {
	if e == nil {
		return 0, false
	}
	v, ok := e.Limits[metric]
	return v, ok
}

func parseEntitlements(row gen.GetOrgEntitlementsRow) *Entitlements {
	e := &Entitlements{
		PlanCode: row.Code, PlanName: row.Name, Status: row.Status,
		Features: map[string]bool{}, Limits: map[string]int64{},
	}
	var feats []string
	_ = json.Unmarshal(row.Features, &feats)
	for _, f := range feats {
		e.Features[f] = true
	}
	_ = json.Unmarshal(row.Limits, &e.Limits)
	return e
}

// Service resolves entitlements (cached — they sit on hot request paths) and
// records usage.
type Service struct {
	q   *gen.Queries
	ttl time.Duration

	mu      sync.Mutex
	entries map[int64]entry
}

type entry struct {
	ent   *Entitlements // nil = org has no plan row (unmetered)
	until time.Time
}

func NewService(q *gen.Queries, ttl time.Duration) *Service {
	return &Service{q: q, ttl: ttl, entries: make(map[int64]entry)}
}

// EntitlementsFor returns the org's entitlements, nil when the org has no plan
// row (unmetered) — and also nil on transient DB errors (enforcement fails
// open; a billing blip must not take commerce down).
func (s *Service) EntitlementsFor(ctx context.Context, orgID int64) *Entitlements {
	s.mu.Lock()
	if e, ok := s.entries[orgID]; ok && time.Now().Before(e.until) {
		s.mu.Unlock()
		return e.ent
	}
	s.mu.Unlock()

	var ent *Entitlements
	if row, err := s.q.GetOrgEntitlements(ctx, orgID); err == nil {
		ent = parseEntitlements(row)
	}
	s.mu.Lock()
	s.entries[orgID] = entry{ent: ent, until: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return ent
}

// Invalidate drops the cached entry (plan changes apply immediately on this node).
func (s *Service) Invalidate(orgID int64) {
	s.mu.Lock()
	delete(s.entries, orgID)
	s.mu.Unlock()
}

// InvalidateAll empties the cache — editing a PLAN affects every org on it.
func (s *Service) InvalidateAll() {
	s.mu.Lock()
	s.entries = make(map[int64]entry)
	s.mu.Unlock()
}

// Over reports whether adding n to the metric would exceed the org's cap.
func (s *Service) Over(ctx context.Context, ent *Entitlements, orgID int64, metric string, n int64) bool {
	limit, capped := ent.Limit(metric)
	if !capped {
		return false
	}
	current, err := s.q.GetUsageValue(ctx, gen.GetUsageValueParams{
		OrganizationID: orgID, Metric: metric, PeriodKey: PeriodKeyFor(metric, time.Now()),
	})
	if err != nil {
		current = 0 // no row yet
	}
	return current+n > limit
}

// Record adds n to the metric's current-period counter (best-effort — metering
// must never fail the request it measures).
func (s *Service) Record(ctx context.Context, orgID int64, metric string, n int64) {
	if orgID == 0 || n == 0 {
		return
	}
	_, _ = s.q.IncrementUsage(ctx, gen.IncrementUsageParams{
		OrganizationID: orgID, Metric: metric, PeriodKey: PeriodKeyFor(metric, time.Now()), Value: n,
	})
}
