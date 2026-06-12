package billing

import (
	"net/http"
	"strings"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
)

// rule is what the gate enforces for one route: a required plan feature, a
// metered usage metric, or both. bytes meters by Content-Length (media upload)
// instead of by call.
type rule struct {
	feature string
	metric  string
	bytes   bool
}

// ruleFor maps a request to its billing rule. One table, matched before the
// router — premium modules stay billing-unaware.
func ruleFor(method, path string) (rule, bool) {
	post := method == http.MethodPost
	switch {
	case post && (path == "/admin/assistant" || path == "/storefront/assistant"):
		return rule{feature: FeatureAssistant, metric: MetricAICalls}, true
	case post && (path == "/admin/orders" || path == "/storefront/orders"):
		return rule{metric: MetricOrders}, true
	case post && strings.HasPrefix(path, "/storefront/quotes/") && strings.HasSuffix(path, "/accept"):
		return rule{metric: MetricOrders}, true
	case post && path == "/admin/media":
		return rule{metric: MetricStorage, bytes: true}, true
	case strings.HasPrefix(path, "/admin/subscriptions") || strings.HasPrefix(path, "/storefront/subscriptions"):
		return rule{feature: FeatureSubscriptions}, true
	case strings.HasPrefix(path, "/admin/rebates") || strings.HasPrefix(path, "/storefront/rebates"):
		return rule{feature: FeatureRebates}, true
	case strings.HasPrefix(path, "/admin/fx-rates"):
		return rule{feature: FeatureFX}, true
	case strings.HasPrefix(path, "/admin/search-synonyms"),
		strings.HasPrefix(path, "/admin/search-redirects"),
		strings.HasPrefix(path, "/admin/merchandising-rules"):
		return rule{feature: FeatureMerchandising}, true
	}
	return rule{}, false
}

// Gate enforces plan features and usage quotas for authenticated requests and
// records consumption AFTER the handler succeeds (2xx) so failed requests never
// burn quota. Runs after the authenticator; requests without claims pass
// (public routes share the chain, and nothing in the table is public).
func Gate(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := mw.ClaimsFrom(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			ru, ok := ruleFor(r.Method, r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			ent := svc.EntitlementsFor(r.Context(), claims.OrgID)
			if ru.feature != "" && !ent.Allows(ru.feature) {
				response.Fail(w, http.StatusForbidden, "feature_not_in_plan",
					"the "+ru.feature+" feature is not part of this organization's plan")
				return
			}
			if ru.metric == "" {
				next.ServeHTTP(w, r)
				return
			}
			n := int64(1)
			if ru.bytes {
				n = r.ContentLength
				if n < 0 {
					n = 0
				}
			}
			if svc.Over(r.Context(), ent, claims.OrgID, ru.metric, n) {
				response.Fail(w, http.StatusForbidden, "quota_exceeded",
					"this organization's plan limit for "+ru.metric+" has been reached")
				return
			}
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			if sw.status < 300 {
				svc.Record(r.Context(), claims.OrgID, ru.metric, n)
			}
		})
	}
}

// statusWriter captures the response status so the gate records usage only for
// successful requests.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
