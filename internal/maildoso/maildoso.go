// Package maildoso is a client for the Maildoso API (https://developers.maildoso.com),
// used for done-for-you inbox provisioning: register domains, create mailboxes,
// and read back their credentials. Auth is a Personal Access Token (PAT) sent as
// a Bearer token (app.maildoso.com -> Settings -> API Keys).
//
// NOTE: the OpenAPI spec declares no servers block, so the base URL is set here
// and is overridable at runtime via the maildoso_base_url setting.
package maildoso

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

const (
	// Maildoso's Scalar docs use relative URLs, so the REST API is served from the
	// docs origin. (app.* is the SPA; api.*/mcp.* are wildcard hosts with no TLS.)
	DefaultBaseURL = "https://developers.maildoso.com"

	pathMe             = "/v1/user/me"
	pathDomains        = "/v1/user/domains"
	pathAccountsLookup = "/v1/user/accounts-lookup"
	pathAccounts       = "/v1/user/accounts"
)

// Client talks to the Maildoso API with a bearer PAT.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// New builds a client. baseURL may be empty to use DefaultBaseURL.
func New(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	// Fresh connection per request: Maildoso's edge intermittently returns a
	// non-TLS response on some connections, and reusing a poisoned pooled
	// connection would make that sticky.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DisableKeepAlives = true
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second, Transport: tr},
	}
}

// Configured reports whether a PAT is present.
func (c *Client) Configured() bool { return c.apiKey != "" }

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var buf []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		buf = b
	}

	// Retry transport-level failures (Maildoso's edge intermittently answers
	// without a valid TLS handshake). HTTP status errors are not retried.
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}

		var body io.Reader
		if buf != nil {
			body = bytes.NewReader(buf)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		if buf != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err // transport error -> retry
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("%s: http %d: %s", path, resp.StatusCode, snippet(raw))
		}
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("%s: decode: %w", path, err)
			}
		}
		return nil
	}
	return fmt.Errorf("%s: %w (after retries)", path, lastErr)
}

// VerifyKey checks the PAT works and that the base URL points at the real API
// (GET /v1/user/me must return a JSON user, not an SPA's HTML page).
func (c *Client) VerifyKey(ctx context.Context) error {
	var out struct {
		ID *int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, pathMe, nil, &out); err != nil {
		return err
	}
	if out.ID == nil {
		return fmt.Errorf("%s returned no user id — base URL is probably not the API host", pathMe)
	}
	return nil
}

// Domain is a sending domain in the Maildoso account.
type Domain struct {
	ID         int64  `json:"id"`
	DomainName string `json:"domain_name"`
	Status     string `json:"domain_status"`
}

// ListDomains returns the account's domains (GET /v1/user/domains -> array).
func (c *Client) ListDomains(ctx context.Context) ([]Domain, error) {
	var out []Domain
	err := c.do(ctx, http.MethodGet, pathDomains, nil, &out)
	return out, err
}

// CreateDomain registers/buys a domain (POST /v1/user/domains).
func (c *Client) CreateDomain(ctx context.Context, domain string) error {
	return c.do(ctx, http.MethodPost, pathDomains, map[string]any{"domains": []string{domain}}, nil)
}

// Mailbox is a provisioned email account, including the credentials needed to
// connect it. Maildoso exposes the IMAP host/port and the password; SMTP is
// derived by the caller (the API doesn't return an SMTP host directly).
type Mailbox struct {
	ID       int64  `json:"id"`
	Email    string `json:"email_account"`
	Password string `json:"password"`
	Provider string `json:"provider"` // "maildoso" | "google"
	Status   string `json:"status"`
	IMAP     *struct {
		Host string `json:"imap_host"`
		Port int    `json:"port"`
	} `json:"imap"`
}

// ListMailboxes returns all email accounts with credentials
// (GET /v1/user/accounts-lookup -> {items, meta}).
func (c *Client) ListMailboxes(ctx context.Context) ([]Mailbox, error) {
	var out struct {
		Items []Mailbox `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, pathAccountsLookup, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// CreateMailboxes provisions email accounts for full addresses on a domain
// (POST /v1/user/accounts). provider is "maildoso" or "google".
func (c *Client) CreateMailboxes(ctx context.Context, emails []string, provider string) error {
	type insert struct {
		EmailAccount string `json:"email_account"`
		Provider     string `json:"provider"`
	}
	body := make([]insert, 0, len(emails))
	for _, e := range emails {
		body = append(body, insert{EmailAccount: e, Provider: provider})
	}
	return c.do(ctx, http.MethodPost, pathAccounts, body, nil)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
