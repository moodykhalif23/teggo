package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ClaudePageDesigner authors a page block tree with the Anthropic Messages API.
// It is selected only when AI_PROVIDER=claude and a key is set; otherwise the
// deterministic template designer is used.
type ClaudePageDesigner struct {
	apiKey string
	model  string
	client *http.Client
}

func NewClaudePageDesigner(apiKey, model string) *ClaudePageDesigner {
	if model == "" {
		model = "claude-opus-4-8"
	}
	return &ClaudePageDesigner{apiKey: apiKey, model: model, client: &http.Client{Timeout: 45 * time.Second}}
}

func (ClaudePageDesigner) Name() string { return "claude" }

func (c *ClaudePageDesigner) Design(ctx context.Context, req DesignRequest) ([]Block, string, error) {
	var cat strings.Builder
	if len(req.Categories) == 0 {
		cat.WriteString(" (none exist — do not emit product-grid blocks)")
	} else {
		for _, ct := range req.Categories {
			fmt.Fprintf(&cat, "\n  - id %d: %s", ct.ID, ct.Name)
		}
	}
	system := "You are a storefront page designer for the Teggo B2B commerce platform. " +
		"Given a brief, design a clean, conversion-oriented page as an ordered list of blocks.\n\n" +
		blockSchemaDoc + "\n\nRules:\n" +
		"- Respond with ONLY a JSON array of blocks. No prose, no markdown, no code fences.\n" +
		"- Every block needs a unique \"id\" (e.g. \"b1\", \"b2\").\n" +
		"- Use product-grid ONLY with a category_id from this list:" + cat.String() + "\n" +
		"- Keep rich-text HTML simple: <p>, <h2>, <ul><li>, <strong>."

	resp, err := anthropicMessages(ctx, c.client, c.apiKey, map[string]any{
		"model":      c.model,
		"max_tokens": 2048,
		"system":     system,
		"messages":   []map[string]any{{"role": "user", "content": req.Prompt}},
	})
	if err != nil {
		return nil, "", err
	}
	text := stripFences(firstText(resp.Content))
	var blocks []Block
	if err := json.Unmarshal([]byte(text), &blocks); err != nil {
		return nil, "", fmt.Errorf("designer: model did not return a JSON block array: %w", err)
	}
	return blocks, "Generated with Claude.", nil
}

// stripFences unwraps a fenced/wrapped response down to the bare JSON array.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	if a, b := strings.IndexByte(s, '['), strings.LastIndexByte(s, ']'); a >= 0 && b > a {
		s = s[a : b+1]
	}
	return strings.TrimSpace(s)
}
