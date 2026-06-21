// Package maildoso is a client for the Maildoso provisioning API, which creates
// sending domains and mailboxes (with SPF/DKIM/DMARC configured) and returns
// SMTP/IMAP credentials. PipelineBuilder uses it for done-for-you inbox setup.
//
// NOTE: Maildoso's developer docs (developers.maildoso.com) are behind bot
// protection, so the exact base URL, auth header, and paths below are the single
// place to confirm against the live API. They can also be overridden at runtime
// via the maildoso_base_url setting without a code change.
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

// --- API surface to verify against developers.maildoso.com ---
const (
	DefaultBaseURL = "https://api.maildoso.com/v1"

	pathDomains   = "/domains"   // GET list, POST create
	pathMailboxes = "/mailboxes" // GET list, POST create
)

// Client talks to the Maildoso API with a bearer API key.
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
	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Configured reports whether an API key is present.
func (c *Client) Configured() bool { return c.apiKey != "" }

// do performs a JSON request and decodes the response into out.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 400 {
		return fmt.Errorf("maildoso: http %d: %s", resp.StatusCode, snippet(raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("maildoso: decode response: %w", err)
		}
	}
	return nil
}

// Domain is a sending domain known to Maildoso.
type Domain struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Status string `json:"status"`
}

// Mailbox is a provisioned inbox, including the credentials needed to send/receive.
type Mailbox struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Password string `json:"password"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	IMAPHost string `json:"imap_host"`
	IMAPPort int    `json:"imap_port"`
	Status   string `json:"status"` // e.g. provisioning|active
	DomainID string `json:"domain_id"`
}

// list envelopes tolerate either a bare array or a {"data":[...]} wrapper.
type domainList struct {
	Data []Domain `json:"data"`
}
type mailboxList struct {
	Data []Mailbox `json:"data"`
}

// VerifyKey checks the API key works by listing domains.
func (c *Client) VerifyKey(ctx context.Context) error {
	_, err := c.ListDomains(ctx)
	return err
}

// ListDomains returns the domains in the Maildoso account.
func (c *Client) ListDomains(ctx context.Context) ([]Domain, error) {
	var wrapped domainList
	if err := c.do(ctx, http.MethodGet, pathDomains, nil, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

// CreateDomain registers/connects a domain in Maildoso.
func (c *Client) CreateDomain(ctx context.Context, domain string) (Domain, error) {
	var out Domain
	err := c.do(ctx, http.MethodPost, pathDomains, map[string]any{"domain": domain}, &out)
	return out, err
}

// ListMailboxes returns all mailboxes (with credentials when available).
func (c *Client) ListMailboxes(ctx context.Context) ([]Mailbox, error) {
	var wrapped mailboxList
	if err := c.do(ctx, http.MethodGet, pathMailboxes, nil, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

// MailboxSpec describes one mailbox to create.
type MailboxSpec struct {
	LocalPart string `json:"local_part"` // the part before @, e.g. "jane"
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// CreateMailboxes provisions mailboxes on a domain and returns them. Provisioning
// may be asynchronous; callers should also poll ListMailboxes for credentials.
func (c *Client) CreateMailboxes(ctx context.Context, domainID string, specs []MailboxSpec) ([]Mailbox, error) {
	var wrapped mailboxList
	body := map[string]any{"domain_id": domainID, "mailboxes": specs}
	if err := c.do(ctx, http.MethodPost, pathMailboxes, body, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
