// Package verify checks whether an email address is deliverable using a
// third-party verification provider. Providers are pluggable; the active one and
// its API key come from user settings. All providers normalize to a small set of
// statuses so the rest of the app is provider-agnostic.
package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Normalized verification statuses.
const (
	StatusValid    = "valid"
	StatusInvalid  = "invalid"
	StatusRisky    = "risky"
	StatusCatchAll = "catch_all"
	StatusUnknown  = "unknown"
)

// Verifier checks a single email address.
type Verifier interface {
	Verify(ctx context.Context, email string) (status string, raw string, err error)
	// Preflight validates the key/quota before a batch run so problems (e.g. out
	// of credits) surface to the user immediately. Returns nil if good to go.
	Preflight(ctx context.Context) error
}

// ErrOutOfCredits indicates the provider account has no verification credits.
var ErrOutOfCredits = errors.New("out of email-verification credits")

// IsOutOfCredits reports whether an error is (or wraps) an out-of-credits error.
func IsOutOfCredits(err error) bool {
	return errors.Is(err, ErrOutOfCredits)
}

// New builds a Verifier for the named provider with the given API key.
func New(provider, apiKey string) (Verifier, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("no verification API key configured")
	}
	c := &http.Client{Timeout: 30 * time.Second}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "zerobounce":
		return &zeroBounce{key: apiKey, http: c}, nil
	case "millionverifier":
		return &millionVerifier{key: apiKey, http: c}, nil
	case "neverbounce":
		return &neverBounce{key: apiKey, http: c}, nil
	case "bouncer":
		return &bouncer{key: apiKey, http: c}, nil
	default:
		return nil, fmt.Errorf("unknown verification provider %q", provider)
	}
}

func getJSON(ctx context.Context, c *http.Client, req *http.Request, out any) (string, error) {
	resp, err := c.Do(req.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var buf strings.Builder
	dec := json.NewDecoder(io.TeeReader(resp.Body, &buf))
	if resp.StatusCode >= 400 {
		return buf.String(), fmt.Errorf("verifier http %d", resp.StatusCode)
	}
	if err := dec.Decode(out); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

// --- MillionVerifier: GET /api/v3/?api=KEY&email=E -> {"result":"ok|catch_all|unknown|invalid|disposable"} ---

type millionVerifier struct {
	key  string
	http *http.Client
}

func (m *millionVerifier) Preflight(ctx context.Context) error { return nil }

func (m *millionVerifier) Verify(ctx context.Context, email string) (string, string, error) {
	u := "https://api.millionverifier.com/api/v3/?api=" + url.QueryEscape(m.key) + "&email=" + url.QueryEscape(email)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	var r struct {
		Result string `json:"result"`
	}
	raw, err := getJSON(ctx, m.http, req, &r)
	if err != nil {
		return StatusUnknown, raw, err
	}
	switch strings.ToLower(r.Result) {
	case "ok":
		return StatusValid, raw, nil
	case "invalid", "disposable":
		return StatusInvalid, raw, nil
	case "catch_all":
		return StatusCatchAll, raw, nil
	default:
		return StatusUnknown, raw, nil
	}
}

// --- ZeroBounce: GET /v2/validate?api_key=KEY&email=E -> {"status":"valid|invalid|catch-all|spamtrap|abuse|do_not_mail|unknown"} ---

type zeroBounce struct {
	key  string
	http *http.Client
}

// Preflight checks the ZeroBounce credit balance so an out-of-credits or bad-key
// situation is reported before the batch starts.
func (z *zeroBounce) Preflight(ctx context.Context) error {
	u := "https://api.zerobounce.net/v2/getcredits?api_key=" + url.QueryEscape(z.key)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	var r struct {
		Credits string `json:"Credits"`
	}
	if _, err := getJSON(ctx, z.http, req, &r); err != nil {
		return fmt.Errorf("could not reach ZeroBounce: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(r.Credits))
	if err != nil {
		return nil // unexpected shape — don't block the run on a parse hiccup
	}
	if n < 0 {
		return fmt.Errorf("invalid ZeroBounce API key")
	}
	if n == 0 {
		return fmt.Errorf("you're out of ZeroBounce credits — top up at zerobounce.net to verify leads: %w", ErrOutOfCredits)
	}
	return nil
}

func (z *zeroBounce) Verify(ctx context.Context, email string) (string, string, error) {
	u := "https://api.zerobounce.net/v2/validate?api_key=" + url.QueryEscape(z.key) + "&email=" + url.QueryEscape(email)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	var r struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	raw, err := getJSON(ctx, z.http, req, &r)
	if err != nil {
		return StatusUnknown, raw, err
	}
	if r.Error != "" {
		if strings.Contains(strings.ToLower(r.Error), "credit") {
			return StatusUnknown, raw, fmt.Errorf("%s: %w", r.Error, ErrOutOfCredits)
		}
		return StatusUnknown, raw, fmt.Errorf("ZeroBounce: %s", r.Error)
	}
	switch strings.ToLower(r.Status) {
	case "valid":
		return StatusValid, raw, nil
	case "invalid":
		return StatusInvalid, raw, nil
	case "catch-all":
		return StatusCatchAll, raw, nil
	case "spamtrap", "abuse", "do_not_mail":
		return StatusRisky, raw, nil
	default:
		return StatusUnknown, raw, nil
	}
}

// --- NeverBounce: GET /v4/single/check?key=KEY&email=E -> {"result":"valid|invalid|disposable|catchall|unknown"} ---

type neverBounce struct {
	key  string
	http *http.Client
}

func (n *neverBounce) Preflight(ctx context.Context) error { return nil }

func (n *neverBounce) Verify(ctx context.Context, email string) (string, string, error) {
	u := "https://api.neverbounce.com/v4/single/check?key=" + url.QueryEscape(n.key) + "&email=" + url.QueryEscape(email)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	var r struct {
		Result string `json:"result"`
	}
	raw, err := getJSON(ctx, n.http, req, &r)
	if err != nil {
		return StatusUnknown, raw, err
	}
	switch strings.ToLower(r.Result) {
	case "valid":
		return StatusValid, raw, nil
	case "invalid", "disposable":
		return StatusInvalid, raw, nil
	case "catchall":
		return StatusCatchAll, raw, nil
	default:
		return StatusUnknown, raw, nil
	}
}

// --- Bouncer: GET /v1.1/email/verify?email=E (header x-api-key) -> {"status":"deliverable|undeliverable|risky|unknown"} ---

type bouncer struct {
	key  string
	http *http.Client
}

func (b *bouncer) Preflight(ctx context.Context) error { return nil }

func (b *bouncer) Verify(ctx context.Context, email string) (string, string, error) {
	u := "https://api.usebouncer.com/v1.1/email/verify?email=" + url.QueryEscape(email)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("x-api-key", b.key)
	var r struct {
		Status string `json:"status"`
	}
	raw, err := getJSON(ctx, b.http, req, &r)
	if err != nil {
		return StatusUnknown, raw, err
	}
	switch strings.ToLower(r.Status) {
	case "deliverable":
		return StatusValid, raw, nil
	case "undeliverable":
		return StatusInvalid, raw, nil
	case "risky":
		return StatusRisky, raw, nil
	default:
		return StatusUnknown, raw, nil
	}
}
