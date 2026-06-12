package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration, loaded from the environment.
type Config struct {
	DatabaseURL string
	HTTPPort    string
	JWTSecret   string
	JWTTTL      time.Duration
	Env         string
	LogLevel    string
	// GotenbergURL is the base URL of the Gotenberg PDF service (e.g.
	// http://gotenberg:3000). Empty falls back to a stub PDF renderer.
	GotenbergURL string

	// SMTP transport for transactional email. When SMTPHost is empty the worker
	// uses a log transport (prints emails) so the flow runs without a server.
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	EmailFrom    string

	// PaymentsGateway selects the card processor ("mock" — default — or a real
	// provider like "stripe"/"mpesa" once configured).
	PaymentsGateway string

	// Connection-pool tuning. DBMaxConns caps concurrent DB connections per
	// process; DBMaxConnIdleTime recycles idle connections.
	DBMaxConns        int32
	DBMaxConnIdleTime time.Duration

	// MediaRoot is the filesystem directory for the DAM blob store. In a
	// multi-node deploy this must be a shared volume (or swap in object storage).
	MediaRoot string

	// Integration (Punchout/EDI). PunchoutStorefrontURL is where a buyer lands
	// after punchout start; EDISenderID is our identity on outbound cXML/EDI;
	// PunchoutTTL bounds a punchout session's lifetime.
	PunchoutStorefrontURL string
	EDISenderID           string
	PunchoutTTL           time.Duration

	// AI assistant. AIProvider selects the decision engine for the copilot:
	// "deterministic" (default — a local intent/slot engine, fully reproducible,
	// no external calls), "claude" (the Anthropic Messages API, used only when
	// AnthropicAPIKey is set), or "openai" (any OpenAI-compatible chat endpoint —
	// Groq, Together, Ollama, vLLM — used only when AIChatAPIKey is set). The
	// deterministic engine is always the fallback.
	AIProvider      string
	AnthropicAPIKey string
	AIModel         string

	// OpenAI-compatible chat endpoint for AIProvider=openai. BaseURL is the API
	// root up to /chat/completions (default: Groq).
	AIChatBaseURL string
	AIChatAPIKey  string
	AIChatModel   string

	// CORSAllowedOrigins lists browser origins permitted to call the API
	// cross-origin (the SSR storefront's client-side calls). Comma-separated in
	// CORS_ALLOWED_ORIGINS; defaults to the local dev frontends. Set to "*" to
	// allow any origin (not recommended in production).
	CORSAllowedOrigins []string

	// Pusher Channels powers real-time in-app notifications. When any of these is
	// empty the system runs in poll-only mode (notifications still persist and
	// are served over HTTP; dashboards refresh on a timer instead of instantly).
	// Key + Cluster are public (handed to the browser); Secret + AppID are not.
	PusherAppID   string
	PusherKey     string
	PusherSecret  string
	PusherCluster string
}

// Load reads configuration from environment variables, applying defaults and
// validating required values.
func Load() (Config, error) {
	c := Config{
		DatabaseURL:  getenv("DATABASE_URL", ""),
		HTTPPort:     getenv("HTTP_PORT", "8080"),
		JWTSecret:    getenv("JWT_SECRET", ""),
		Env:          getenv("ENV", "development"),
		LogLevel:     getenv("LOG_LEVEL", "info"),
		GotenbergURL: getenv("GOTENBERG_URL", ""),

		SMTPHost:     getenv("SMTP_HOST", ""),
		SMTPPort:     getenv("SMTP_PORT", "587"),
		SMTPUsername: getenv("SMTP_USERNAME", ""),
		SMTPPassword: getenv("SMTP_PASSWORD", ""),
		EmailFrom:    getenv("EMAIL_FROM", "Teggo <no-reply@teggo.local>"),

		PaymentsGateway: getenv("PAYMENTS_GATEWAY", "mock"),

		DBMaxConns: int32(getenvInt("DB_MAX_CONNS", 20)),
		MediaRoot:  getenv("MEDIA_ROOT", "/data/media"),

		PunchoutStorefrontURL: getenv("PUNCHOUT_STOREFRONT_URL", "/"),
		EDISenderID:           getenv("EDI_SENDER_ID", "TEGGO"),

		AIProvider:      getenv("AI_PROVIDER", "deterministic"),
		AnthropicAPIKey: getenv("ANTHROPIC_API_KEY", ""),
		AIModel:         getenv("AI_MODEL", "claude-opus-4-8"),

		AIChatBaseURL: getenv("AI_CHAT_BASE_URL", "https://api.groq.com/openai/v1"),
		AIChatAPIKey:  getenv("AI_CHAT_API_KEY", ""),
		AIChatModel:   getenv("AI_CHAT_MODEL", "llama-3.3-70b-versatile"),

		CORSAllowedOrigins: splitList(getenv("CORS_ALLOWED_ORIGINS",
			"http://localhost:3000,http://localhost:5173,http://localhost:5174")),

		PusherAppID:   getenv("PUSHER_APP_ID", ""),
		PusherKey:     getenv("PUSHER_KEY", ""),
		PusherSecret:  getenv("PUSHER_SECRET", ""),
		PusherCluster: getenv("PUSHER_CLUSTER", ""),
	}

	ttl, err := time.ParseDuration(getenv("JWT_TTL", "24h"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid JWT_TTL: %w", err)
	}
	c.JWTTTL = ttl

	idle, err := time.ParseDuration(getenv("DB_MAX_CONN_IDLE_TIME", "5m"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid DB_MAX_CONN_IDLE_TIME: %w", err)
	}
	c.DBMaxConnIdleTime = idle

	pttl, err := time.ParseDuration(getenv("PUNCHOUT_TTL", "1h"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid PUNCHOUT_TTL: %w", err)
	}
	c.PunchoutTTL = pttl

	if c.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if c.JWTSecret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitList parses a comma-separated env value into a trimmed, non-empty slice.
func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// getenvInt reads an integer env var, falling back to def when unset or
// unparseable.
func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
