package billing

import (
	"net/http"
	"testing"
	"time"
)

func TestPeriodKeyFor(t *testing.T) {
	at := time.Date(2026, 6, 12, 23, 59, 0, 0, time.UTC)
	if k := PeriodKeyFor(MetricOrders, at); k != "2026-06" {
		t.Errorf("orders key: %s", k)
	}
	if k := PeriodKeyFor(MetricAICalls, at); k != "2026-06" {
		t.Errorf("ai key: %s", k)
	}
	if k := PeriodKeyFor(MetricStorage, at); k != "all" {
		t.Errorf("storage key: %s", k)
	}
}

func TestEntitlementsNilAllowsAll(t *testing.T) {
	var e *Entitlements
	if !e.Allows(FeatureRebates) {
		t.Error("nil entitlements must allow (unmetered org)")
	}
	if _, capped := e.Limit(MetricOrders); capped {
		t.Error("nil entitlements must be unlimited")
	}
}

func TestEntitlementsFeaturesAndLimits(t *testing.T) {
	e := &Entitlements{
		Features: map[string]bool{FeatureAssistant: true},
		Limits:   map[string]int64{MetricOrders: 50},
	}
	if !e.Allows(FeatureAssistant) || e.Allows(FeatureRebates) {
		t.Error("feature flags wrong")
	}
	if v, ok := e.Limit(MetricOrders); !ok || v != 50 {
		t.Errorf("orders limit: %d %v", v, ok)
	}
	if _, ok := e.Limit(MetricStorage); ok {
		t.Error("absent metric must be unlimited")
	}
}

func TestRuleTable(t *testing.T) {
	cases := []struct {
		method, path    string
		feature, metric string
		want            bool
	}{
		{http.MethodPost, "/admin/orders", "", MetricOrders, true},
		{http.MethodPost, "/storefront/orders", "", MetricOrders, true},
		{http.MethodPost, "/storefront/quotes/abc/accept", "", MetricOrders, true},
		{http.MethodPost, "/admin/orders/5/shipments", "", "", false}, // sub-resources are NOT new orders
		{http.MethodGet, "/admin/orders", "", "", false},              // reads are never metered
		{http.MethodPost, "/admin/media", "", MetricStorage, true},
		{http.MethodPost, "/admin/assistant", FeatureAssistant, MetricAICalls, true},
		{http.MethodGet, "/admin/subscriptions", FeatureSubscriptions, "", true},
		{http.MethodGet, "/storefront/rebates", FeatureRebates, "", true},
		{http.MethodGet, "/admin/fx-rates", FeatureFX, "", true},
		{http.MethodPut, "/admin/merchandising-rules/1", FeatureMerchandising, "", true},
		{http.MethodGet, "/admin/search-synonyms", FeatureMerchandising, "", true},
		{http.MethodGet, "/admin/products", "", "", false},
	}
	for _, c := range cases {
		ru, ok := ruleFor(c.method, c.path)
		if ok != c.want {
			t.Errorf("%s %s: matched=%v want %v", c.method, c.path, ok, c.want)
			continue
		}
		if ok && (ru.feature != c.feature || ru.metric != c.metric) {
			t.Errorf("%s %s: rule %+v, want feature=%q metric=%q", c.method, c.path, ru, c.feature, c.metric)
		}
	}
}
