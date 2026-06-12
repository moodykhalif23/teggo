// Package merchandising adds search curation on top of Postgres FTS: synonym
// query-expansion and pin/boost/bury reordering. Pure and DB-free — callers load
// the rules and pass them in. Reordering operates on a result page's product ids.
package merchandising

import (
	"sort"
	"strings"
)

func ExpandQuery(query string, synonyms map[string]string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return q
	}
	exp, ok := synonyms[strings.ToLower(q)]
	if !ok {
		return q
	}
	parts := []string{q}
	for _, w := range strings.FieldsFunc(exp, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' }) {
		if w = strings.TrimSpace(w); w != "" {
			parts = append(parts, w)
		}
	}
	if len(parts) == 1 {
		return q
	}
	return strings.Join(parts, " OR ")
}

// Rule is a single pin/boost/bury directive for a product.
type Rule struct {
	ProductID int64
	Action    string // "pin" | "boost" | "bury"
	Position  int32  // ordering among pins
}

// Reorder applies merchandising rules to a result page's product ids, operating
// only on ids already present (v1: page-level, no force-include of non-matching
// products). Final order: pinned (by position) → boosted → untouched (original
// order) → buried. A product pinned and buried is treated as pinned.
func Reorder(ids []int64, rules []Rule) []int64 {
	if len(rules) == 0 || len(ids) == 0 {
		return ids
	}
	present := make(map[int64]bool, len(ids))
	for _, id := range ids {
		present[id] = true
	}

	var pins []Rule
	pinned := map[int64]bool{}
	boost := map[int64]bool{}
	bury := map[int64]bool{}
	for _, r := range rules {
		if !present[r.ProductID] {
			continue
		}
		switch r.Action {
		case "pin":
			if !pinned[r.ProductID] {
				pinned[r.ProductID] = true
				pins = append(pins, r)
			}
		case "boost":
			boost[r.ProductID] = true
		case "bury":
			bury[r.ProductID] = true
		}
	}
	if len(pins) == 0 && len(boost) == 0 && len(bury) == 0 {
		return ids
	}
	sort.SliceStable(pins, func(i, j int) bool { return pins[i].Position < pins[j].Position })

	out := make([]int64, 0, len(ids))
	for _, p := range pins {
		out = append(out, p.ProductID)
	}
	var boosted, normal, buried []int64
	for _, id := range ids {
		if pinned[id] {
			continue
		}
		switch {
		case bury[id]:
			buried = append(buried, id)
		case boost[id]:
			boosted = append(boosted, id)
		default:
			normal = append(normal, id)
		}
	}
	out = append(out, boosted...)
	out = append(out, normal...)
	out = append(out, buried...)
	return out
}
