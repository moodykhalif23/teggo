package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// mockOpenAI runs an httptest server speaking the OpenAI chat-completions wire
// shape. Each call captures the request body for assertions and returns the
// canned response.
func mockOpenAI(t *testing.T, status int, response string) (*OpenAIProvider, *map[string]any) {
	t.Helper()
	var lastReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path: got %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("authorization"); got != "Bearer test-key" {
			t.Errorf("authorization: got %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&lastReq); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return NewOpenAIProvider(srv.URL, "test-key", "test-model"), &lastReq
}

func TestOpenAIProviderDecideToolCall(t *testing.T) {
	p, lastReq := mockOpenAI(t, http.StatusOK, `{
		"choices": [{"message": {
			"content": "",
			"tool_calls": [{"type": "function", "function": {
				"name": "order_status",
				"arguments": "{\"public_id\": \"3fa85f64\"}"
			}}]
		}}]
	}`)

	tool := fakeTool{name: "order_status", desc: "Check an order", audience: "storefront"}
	d, err := p.Decide(context.Background(), "where is order 3fa85f64?", nil, []Tool{tool})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if d.Tool != "order_status" {
		t.Errorf("tool: got %q", d.Tool)
	}
	if d.Args["public_id"] != "3fa85f64" {
		t.Errorf("args: got %v", d.Args)
	}

	// The request must advertise the tool catalog in OpenAI function shape and
	// lead with the shared system prompt.
	req := *lastReq
	tools, _ := req["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("request tools: got %v", req["tools"])
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "order_status" {
		t.Errorf("request tool name: got %v", fn["name"])
	}
	msgs := req["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(first["content"].(string), "Teggo B2B commerce assistant") {
		t.Errorf("first message should be the system prompt, got %v", first)
	}
}

func TestOpenAIProviderDecideTextReply(t *testing.T) {
	p, _ := mockOpenAI(t, http.StatusOK, `{
		"choices": [{"message": {"content": "You have 3 open quotes."}}]
	}`)
	d, err := p.Decide(context.Background(), "how many quotes?", nil, nil)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if d.Tool != "" || d.Reply != "You have 3 open quotes." {
		t.Errorf("decision: got %+v", d)
	}
}

func TestOpenAIProviderDecideMalformedArgsDegradeToEmpty(t *testing.T) {
	p, _ := mockOpenAI(t, http.StatusOK, `{
		"choices": [{"message": {"tool_calls": [{"type": "function",
			"function": {"name": "orders", "arguments": "not-json"}}]}}]
	}`)
	d, err := p.Decide(context.Background(), "orders", nil, nil)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if d.Tool != "orders" || len(d.Args) != 0 {
		t.Errorf("want tool with empty args, got %+v", d)
	}
}

func TestOpenAIProviderCompose(t *testing.T) {
	p, lastReq := mockOpenAI(t, http.StatusOK, `{
		"choices": [{"message": {"content": "Order ABC is delivered."}}]
	}`)
	tool := fakeTool{name: "order_status"}
	text, err := p.Compose(context.Background(), "where is my order?", tool,
		ToolResult{Summary: "Order ABC is delivered.", Data: map[string]any{"status": "delivered"}}, nil)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if text != "Order ABC is delivered." {
		t.Errorf("text: got %q", text)
	}
	// The compose prompt must carry the tool result for grounding.
	msgs := (*lastReq)["messages"].([]any)
	user := msgs[len(msgs)-1].(map[string]any)["content"].(string)
	if !strings.Contains(user, `"status":"delivered"`) {
		t.Errorf("compose prompt should embed the tool result, got %q", user)
	}
}

func TestOpenAIProviderErrorSurfacesBody(t *testing.T) {
	p, _ := mockOpenAI(t, http.StatusBadRequest, `{"error": {"message": "model decommissioned"}}`)
	_, err := p.Decide(context.Background(), "hi", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "status 400") || !strings.Contains(err.Error(), "model decommissioned") {
		t.Errorf("want status + body snippet in error, got %v", err)
	}
}

func TestNewOpenAIProviderDefaults(t *testing.T) {
	p := NewOpenAIProvider("", "k", "")
	if p.baseURL != "https://api.groq.com/openai/v1" {
		t.Errorf("default baseURL: got %q", p.baseURL)
	}
	if p.model != "llama-3.3-70b-versatile" {
		t.Errorf("default model: got %q", p.model)
	}
	// Trailing slash is normalized so /chat/completions concatenation is clean.
	if p2 := NewOpenAIProvider("http://x/v1/", "k", "m"); p2.baseURL != "http://x/v1" {
		t.Errorf("trim slash: got %q", p2.baseURL)
	}
}

// TestOpenAIProviderLiveSmoke talks to the real configured endpoint (Groq).
// Guarded so the suite stays offline: runs only with AI_SMOKE=1 and an API key
// in the environment.
//
//	AI_SMOKE=1 AI_CHAT_API_KEY=... go test ./internal/ai -run LiveSmoke -v
func TestOpenAIProviderLiveSmoke(t *testing.T) {
	if os.Getenv("AI_SMOKE") != "1" {
		t.Skip("set AI_SMOKE=1 (and AI_CHAT_API_KEY) to run the live smoke test")
	}
	key := os.Getenv("AI_CHAT_API_KEY")
	if key == "" {
		t.Skip("AI_CHAT_API_KEY not set")
	}
	p := NewOpenAIProvider(os.Getenv("AI_CHAT_BASE_URL"), key, os.Getenv("AI_CHAT_MODEL"))

	tool := fakeTool{name: "order_status", desc: "Look up the status of an order by its public id",
		audience: "storefront"}
	d, err := p.Decide(context.Background(), "what's the status of order 3fa85f64?", nil, []Tool{tool})
	if err != nil {
		t.Fatalf("live decide: %v", err)
	}
	t.Logf("live decide: tool=%q args=%v reply=%q", d.Tool, d.Args, d.Reply)
	if d.Tool != "order_status" {
		t.Errorf("expected the model to pick order_status, got tool=%q reply=%q", d.Tool, d.Reply)
	}

	text, err := p.Compose(context.Background(), "what's the status of order 3fa85f64?", tool,
		ToolResult{Summary: "Order 3fa85f64 is delivered.", Data: map[string]any{"status": "delivered"}}, nil)
	if err != nil {
		t.Fatalf("live compose: %v", err)
	}
	t.Logf("live compose: %q", text)
	if text == "" {
		t.Error("live compose returned empty text")
	}
}
