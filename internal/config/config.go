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
}

// Load reads configuration from environment variables, applying defaults and
// validating required values.
func Load() (Config, error) {
	c := Config{
		DatabaseURL: getenv("DATABASE_URL", ""),
		HTTPPort:    getenv("HTTP_PORT", "8080"),
		JWTSecret:   getenv("JWT_SECRET", ""),
		Env:         getenv("ENV", "development"),
		LogLevel:    getenv("LOG_LEVEL", "info"),
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
