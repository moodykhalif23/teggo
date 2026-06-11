package ai

import (
	"context"
	"fmt"
	"strings"
)

// DeterministicPageDesigner builds a coherent page from keyword heuristics with
// no external calls — fully reproducible and the default when no API key is set.
// It always lays down a hero + intro, and conditionally adds a promo banner,
// a product grid (mapped to a real category), and a closing call-to-action.
type DeterministicPageDesigner struct{}

func NewDeterministicPageDesigner() *DeterministicPageDesigner { return &DeterministicPageDesigner{} }

func (DeterministicPageDesigner) Name() string { return "deterministic" }

func (DeterministicPageDesigner) Design(_ context.Context, req DesignRequest) ([]Block, string, error) {
	lp := strings.ToLower(req.Prompt)
	var blocks []Block
	n := 0
	id := func() string { n++; return fmt.Sprintf("g%d", n) }

	wantsProducts := containsAny(lp, "product", "catalog", "shop", "browse", "range", "grid",
		"collection", "equipment", "tools", "supplies", "store")

	// Hero — headline derived from the brief.
	hero := Block{"type": "hero", "id": id(), "props": map[string]any{
		"heading":    headline(req.Prompt),
		"subheading": "Trusted B2B supply, built for your business.",
	}}
	if wantsProducts {
		hero["props"].(map[string]any)["cta"] = link("Browse products", "/")
	}
	blocks = append(blocks, hero)

	// Promo banner — only when the brief implies a sale/offer.
	if containsAny(lp, "sale", "promo", "offer", "discount", "deal", "clearance", "save") {
		blocks = append(blocks, Block{"type": "banner", "id": id(), "props": map[string]any{
			"heading": "Limited-time offer — save on selected items.",
			"cta":     link("See deals", "/"),
		}})
	}

	// Intro copy.
	blocks = append(blocks, Block{"type": "rich-text", "id": id(), "props": map[string]any{
		"html": "<p>" + introCopy(req.Prompt) + "</p>",
	}})

	// Product grid — mapped to a real category when one is available.
	var note string
	if wantsProducts {
		if catID, ok := pickCategory(req.Prompt, req.Categories); ok {
			blocks = append(blocks, Block{"type": "product-grid", "id": id(), "props": map[string]any{
				"heading": "Featured products",
				"source":  map[string]any{"kind": "category", "category_id": catID, "limit": 8},
			}})
		} else {
			note = "No catalog categories exist yet, so a product grid was omitted — create a category to feature products."
		}
	}

	// Closing call-to-action.
	blocks = append(blocks, Block{"type": "cta", "id": id(), "props": map[string]any{
		"heading": "Ready to get started?",
		"cta":     link("Contact sales", "/contact"),
	}})

	notes := "Generated with the offline template engine. Set ANTHROPIC_API_KEY (AI_PROVIDER=claude) for richer, fully AI-authored layouts."
	if note != "" {
		notes = note + " " + notes
	}
	return blocks, notes, nil
}

func link(label, href string) map[string]any { return map[string]any{"label": label, "href": href} }

// headline pulls a short title from the first clause of the brief.
func headline(prompt string) string {
	p := strings.TrimSpace(prompt)
	for _, sep := range []string{".", ",", ":", ";", " - ", " — ", " for ", " with "} {
		if i := strings.Index(strings.ToLower(p), sep); i > 0 {
			p = p[:i]
			break
		}
	}
	words := strings.Fields(p)
	if len(words) > 8 {
		words = words[:8]
	}
	p = strings.TrimSpace(strings.Join(words, " "))
	if p == "" {
		return "Welcome"
	}
	return strings.ToUpper(p[:1]) + p[1:]
}

func introCopy(prompt string) string {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return "Welcome to our store."
	}
	if len(p) > 180 {
		p = p[:180]
	}
	return strings.ToUpper(p[:1]) + p[1:] + "."
}

// pickCategory matches a category whose name appears in the brief, else falls
// back to the first available category.
func pickCategory(prompt string, cats []DesignCategory) (int64, bool) {
	lp := strings.ToLower(prompt)
	for _, c := range cats {
		if c.Name != "" && strings.Contains(lp, strings.ToLower(c.Name)) {
			return c.ID, true
		}
	}
	if len(cats) > 0 {
		return cats[0].ID, true
	}
	return 0, false
}
