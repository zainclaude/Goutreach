package config

import "os"

// Config holds all runtime configuration, loaded from the environment.
type Config struct {
	DatabaseURL   string // Postgres connection string
	HTTPAddr      string // e.g. ":8080"
	AppURL        string // public base URL for tracking links, e.g. https://send.example.com
	AnthropicKey  string // ANTHROPIC_API_KEY
	EncryptionKey string // 32-byte key (hex or raw) for AES-256-GCM at-rest encryption
	JWTSecret     string // signing secret for dashboard sessions

	GoogleClientID     string // OAuth client id for "Sign in with Google"
	GoogleClientSecret string // OAuth client secret
	GoogleRedirectURL  string // OAuth redirect (defaults to APP_URL + /api/oauth/google/callback)
}

// Load reads configuration from environment variables, applying defaults where
// sensible. It understands PaaS conventions: HTTP_ADDR falls back to $PORT, and
// APP_URL falls back to $RENDER_EXTERNAL_URL.
func Load() (Config, error) {
	c := Config{
		DatabaseURL:   env("DATABASE_URL", "postgres://goutreach:goutreach@localhost:5432/goutreach?sslmode=disable"),
		HTTPAddr:      httpAddr(),
		AppURL:        appURL(),
		AnthropicKey:  os.Getenv("ANTHROPIC_API_KEY"),
		EncryptionKey: os.Getenv("ENCRYPTION_KEY"),
		JWTSecret:     env("JWT_SECRET", "dev-insecure-jwt-secret-change-me"),

		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
	}
	if c.GoogleRedirectURL == "" {
		c.GoogleRedirectURL = appURL() + "/api/oauth/google/callback"
	}
	if c.EncryptionKey == "" {
		// Dev-only fallback so the app boots locally; production must set this.
		c.EncryptionKey = "dev-insecure-encryption-key-change-me"
	}
	return c, nil
}

// httpAddr prefers HTTP_ADDR, then $PORT (Render/Railway/Fly), then :8080.
func httpAddr() string {
	if a := os.Getenv("HTTP_ADDR"); a != "" {
		return a
	}
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

// appURL prefers APP_URL, then $RENDER_EXTERNAL_URL, then localhost.
func appURL() string {
	if u := os.Getenv("APP_URL"); u != "" {
		return u
	}
	if u := os.Getenv("RENDER_EXTERNAL_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
