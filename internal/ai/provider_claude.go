package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ClaudeProvider routes the decision through the Anthropic Messages API with the
// SAME tool catalog the deterministic engine uses — the model may only choose
// among the tools advertised, and every chosen tool still runs under the caller's
// scope and guards. It is selected only when AI_PROVIDER=claude and an API key is
// configured; otherwise the deterministic engine is used. (Not exercised in unit
// tests, which run fully offline against the deterministic provider.)
type ClaudeProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewClaudeProvider(apiKey, model string) *ClaudeProvider {
	if model == "" {
		model = "claude-opus-4-8"
	}
	return &ClaudeProvider{apiKey: apiKey, model: model, client: &http.Client{Timeout: 30 * time.Second}}
}

func (ClaudeProvider) Name() string { return "claude" }

const claudeSystem = "You are the Teggo B2B commerce assistant. You help the signed-in user with " +
	"their orders, invoices, quotes, accounts and (for staff) revenue operations. " +
	"Only act through the provided tools — never invent data. Be concise and factual."

func (c *ClaudeProvider) Decide(ctx context.Context, msg string, history []Turn, tools []Tool) (Decision, error) {
	toolDefs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		props := map[string]any{}
		required := []string{}
		for _, p := range t.Params() {
			props[p.Name] = map[string]any{"type": jsonType(p.Type), "description": p.Description}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		toolDefs = append(toolDefs, map[string]any{
			"name":         t.Name(),
			"description":  t.Description(),
			"input_schema": map[string]any{"type": "object", "properties": props, "required": required},
		})
	}

	resp, err := c.post(ctx, map[string]any{
		"model":      c.model,
		"max_tokens": 1024,
		"system":     claudeSystem,
		"tools":      toolDefs,
		"messages":   buildMessages(history, msg),
	})
	if err != nil {
		return Decision{}, err
	}
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			args := map[string]any{}
			_ = json.Unmarshal(block.Input, &args)
			return Decision{Tool: block.Name, Args: args}, nil
		}
	}
	return Decision{Reply: firstText(resp.Content)}, nil
}

func (c *ClaudeProvider) Compose(ctx context.Context, msg string, tool Tool, result ToolResult, history []Turn) (string, error) {
	data, _ := json.Marshal(result)
	prompt := fmt.Sprintf("The user asked: %q.\nYou ran the %q tool, which returned:\n%s\nReply to the user concisely using only this result.", msg, tool.Name(), string(data))
	resp, err := c.post(ctx, map[string]any{
		"model":      c.model,
		"max_tokens": 1024,
		"system":     claudeSystem,
		"messages":   []map[string]any{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", err
	}
	return firstText(resp.Content), nil
}

// ---- Anthropic wire types (minimal) --------------------------------------

type claudeResponse struct {
	Content []claudeBlock `json:"content"`
}

type claudeBlock struct {
	Type  string          `json:"type"` // "text" | "tool_use"
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (c *ClaudeProvider) post(ctx context.Context, body map[string]any) (claudeResponse, error) {
	return anthropicMessages(ctx, c.client, c.apiKey, body)
}

// anthropicMessages POSTs a Messages-API request and decodes the response. Shared
// by the chat provider and the page designer so the wire details live in one place.
func anthropicMessages(ctx context.Context, client *http.Client, apiKey string, body map[string]any) (claudeResponse, error) {
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return claudeResponse{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	res, err := client.Do(req)
	if err != nil {
		return claudeResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return claudeResponse{}, fmt.Errorf("anthropic api: status %d", res.StatusCode)
	}
	var out claudeResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return claudeResponse{}, err
	}
	return out, nil
}

func buildMessages(history []Turn, msg string) []map[string]any {
	msgs := make([]map[string]any, 0, len(history)+1)
	for _, h := range history {
		role := "user"
		if h.Role == "assistant" {
			role = "assistant"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": h.Text})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": msg})
	return msgs
}

func firstText(blocks []claudeBlock) string {
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return b.Text
		}
	}
	return "I don't have an answer for that."
}

func jsonType(t string) string {
	switch t {
	case "int", "number":
		return "number"
	default:
		return "string"
	}
}
