package config

import (
	"fmt"
	"os"
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
	}

	ttl, err := time.ParseDuration(getenv("JWT_TTL", "24h"))
	if err != nil {
		return Config{}, fmt.Errorf("invalid JWT_TTL: %w", err)
	}
	c.JWTTTL = ttl

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
