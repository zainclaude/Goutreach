package config

import (
	"fmt"
	"os"
)

// Config holds all runtime configuration, loaded from the environment.
type Config struct {
	DatabaseURL   string // Postgres connection string
	HTTPAddr      string // e.g. ":8080"
	AppURL        string // public base URL for tracking links, e.g. https://send.example.com
	AnthropicKey  string // ANTHROPIC_API_KEY
	EncryptionKey string // 32-byte key (hex or raw) for AES-256-GCM at-rest encryption
	JWTSecret     string // signing secret for dashboard sessions
}

// Load reads configuration from environment variables, applying defaults where
// sensible and returning an error when a required value is missing.
func Load() (Config, error) {
	c := Config{
		DatabaseURL:   env("DATABASE_URL", "postgres://goutreach:goutreach@localhost:5432/goutreach?sslmode=disable"),
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		AppURL:        env("APP_URL", "http://localhost:8080"),
		AnthropicKey:  os.Getenv("ANTHROPIC_API_KEY"),
		EncryptionKey: os.Getenv("ENCRYPTION_KEY"),
		JWTSecret:     env("JWT_SECRET", "dev-insecure-jwt-secret-change-me"),
	}

	if c.EncryptionKey == "" {
		// Dev-only fallback so the app boots locally; production must set this.
		c.EncryptionKey = "0123456789abcdef0123456789abcdef"
	}
	if len(c.EncryptionKey) < 32 {
		return c, fmt.Errorf("ENCRYPTION_KEY must be at least 32 bytes, got %d", len(c.EncryptionKey))
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
