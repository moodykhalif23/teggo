package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider routes the decision through any OpenAI-compatible chat
// completions endpoint (Groq, Together, Ollama, vLLM, …) with the SAME tool
// catalog the deterministic engine uses — the model may only choose among the
// tools advertised, and every chosen tool still runs under the caller's scope
// and guards. It is selected only when AI_PROVIDER=openai and an API key is
// configured; otherwise the deterministic engine is used. (Unit tests exercise
// it against a local mock server; no network.)
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAIProvider builds a provider for an OpenAI-compatible endpoint.
// baseURL is the API root up to (not including) /chat/completions, e.g.
// https://api.groq.com/openai/v1.
func NewOpenAIProvider(baseURL, apiKey, model string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.groq.com/openai/v1"
	}
	if model == "" {
		model = "llama-3.3-70b-versatile"
	}
	return &OpenAIProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Decide(ctx context.Context, msg string, history []Turn, tools []Tool) (Decision, error) {
	toolDefs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		props := map[string]any{}
		required := []string{}
		for _, par := range t.Params() {
			props[par.Name] = map[string]any{"type": jsonType(par.Type), "description": par.Description}
			if par.Required {
				required = append(required, par.Name)
			}
		}
		toolDefs = append(toolDefs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  map[string]any{"type": "object", "properties": props, "required": required},
			},
		})
	}

	messages := make([]map[string]any, 0, len(history)+2)
	messages = append(messages, map[string]any{"role": "system", "content": assistantSystem})
	messages = append(messages, buildMessages(history, msg)...)

	body := map[string]any{
		"model":      p.model,
		"max_tokens": 1024,
		"messages":   messages,
	}
	if len(toolDefs) > 0 {
		body["tools"] = toolDefs
	}
	resp, err := p.post(ctx, body)
	if err != nil {
		return Decision{}, err
	}
	if len(resp.Choices) == 0 {
		return Decision{}, fmt.Errorf("openai api: empty choices")
	}
	m := resp.Choices[0].Message
	for _, tc := range m.ToolCalls {
		if tc.Function.Name == "" {
			continue
		}
		// OpenAI-shape arguments arrive as a JSON-encoded STRING (unlike
		// Anthropic's object); a malformed payload degrades to empty args.
		args := map[string]any{}
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		return Decision{Tool: tc.Function.Name, Args: args}, nil
	}
	return Decision{Reply: m.Content}, nil
}

func (p *OpenAIProvider) Compose(ctx context.Context, msg string, tool Tool, result ToolResult, history []Turn) (string, error) {
	data, _ := json.Marshal(result)
	prompt := fmt.Sprintf("The user asked: %q.\nYou ran the %q tool, which returned:\n%s\nReply to the user concisely using only this result.", msg, tool.Name(), string(data))
	resp, err := p.post(ctx, map[string]any{
		"model":      p.model,
		"max_tokens": 1024,
		"messages": []map[string]any{
			{"role": "system", "content": assistantSystem},
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("openai api: empty completion")
	}
	return resp.Choices[0].Message.Content, nil
}

// ---- OpenAI wire types (minimal) ------------------------------------------

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIMessage struct {
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls"`
}

type openAIToolCall struct {
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded object
}

func (p *OpenAIProvider) post(ctx context.Context, body map[string]any) (openAIResponse, error) {
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return openAIResponse{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+p.apiKey)
	res, err := p.client.Do(req)
	if err != nil {
		return openAIResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		// Surface a snippet of the body: OpenAI-compatible servers put the real
		// reason (bad model name, decommissioned model, quota) in the JSON error.
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return openAIResponse{}, fmt.Errorf("openai api: status %d: %s", res.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var out openAIResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return openAIResponse{}, err
	}
	return out, nil
}
