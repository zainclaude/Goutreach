// Package porkbun is a small client for the Porkbun v3 JSON API, covering the
// pieces PipelineBuilder needs to provision sending domains: availability +
// pricing checks, domain registration (beta), nameserver control, and DNS record
// management. Every request is an HTTPS POST whose JSON body carries the API
// credentials; see https://porkbun.com/api/json/v3/documentation.
package porkbun

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

const defaultBaseURL = "https://api.porkbun.com/api/json/v3"

// Client talks to the Porkbun API with a fixed key/secret pair.
type Client struct {
	apiKey    string
	secretKey string
	baseURL   string
	http      *http.Client
}

// New builds a client. apiKey/secretKey come from Porkbun (API Access section).
func New(apiKey, secretKey string) *Client {
	return &Client{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseURL:   defaultBaseURL,
		http:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Configured reports whether both credentials are present.
func (c *Client) Configured() bool { return c.apiKey != "" && c.secretKey != "" }

// do POSTs a JSON body to path and decodes the response into out. It returns an
// error when the transport fails or Porkbun reports status != "SUCCESS".
func (c *Client) do(ctx context.Context, path string, body map[string]any, out any) error {
	if body == nil {
		body = map[string]any{}
	}
	body["apikey"] = c.apiKey
	body["secretapikey"] = c.secretKey

	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	// Every Porkbun response includes {"status":"SUCCESS"|"ERROR","message":...}.
	var head struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &head)
	if resp.StatusCode >= 400 || strings.ToUpper(head.Status) == "ERROR" {
		msg := head.Message
		if msg == "" {
			msg = fmt.Sprintf("porkbun http %d", resp.StatusCode)
		}
		return fmt.Errorf("porkbun: %s", msg)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("porkbun: decode response: %w", err)
		}
	}
	return nil
}

// Ping verifies credentials and returns the caller's IP (Porkbun echoes yourIp).
func (c *Client) Ping(ctx context.Context) (string, error) {
	var out struct {
		YourIP string `json:"yourIp"`
	}
	if err := c.do(ctx, "/ping", nil, &out); err != nil {
		return "", err
	}
	return out.YourIP, nil
}

// Availability is the result of a domain availability + price check.
type Availability struct {
	Domain    string `json:"domain"`
	Available bool   `json:"available"`
	Price     string `json:"price"` // first-year registration price
	Premium   bool   `json:"premium"`
}

// CheckDomain reports whether a domain is available and its price. Porkbun rate
// limits this endpoint, so callers should check one domain at a time.
func (c *Client) CheckDomain(ctx context.Context, domain string) (Availability, error) {
	var out struct {
		Response struct {
			Avail   string `json:"avail"` // "yes" | "no"
			Price   string `json:"price"`
			Premium string `json:"premium"`
		} `json:"response"`
	}
	if err := c.do(ctx, "/domain/checkDomain/"+domain, nil, &out); err != nil {
		return Availability{}, err
	}
	return Availability{
		Domain:    domain,
		Available: strings.EqualFold(out.Response.Avail, "yes"),
		Price:     out.Response.Price,
		Premium:   strings.EqualFold(out.Response.Premium, "yes"),
	}, nil
}

// RegisterOptions controls a domain registration.
type RegisterOptions struct {
	Years        int  // registration term, defaults to 1
	WhoisPrivacy bool // enable free WHOIS privacy
	AutoRenew    bool // enable auto-renew
}

// Register purchases a domain through Porkbun. Registration is part of Porkbun's
// beta v3 API and is gated per-TLD; if a TLD is not API-registerable the call
// returns an error and the caller should fall back to manual purchase + Import.
func (c *Client) Register(ctx context.Context, domain string, opt RegisterOptions) error {
	years := opt.Years
	if years < 1 {
		years = 1
	}
	body := map[string]any{
		"years":        years,
		"whoisPrivacy": boolToYN(opt.WhoisPrivacy),
		"autoRenew":    boolToYN(opt.AutoRenew),
	}
	return c.do(ctx, "/domain/register/"+domain, body, nil)
}

// ListDomains returns the domains in the Porkbun account.
func (c *Client) ListDomains(ctx context.Context) ([]string, error) {
	var out struct {
		Domains []struct {
			Domain string `json:"domain"`
		} `json:"domains"`
	}
	if err := c.do(ctx, "/domain/listAll", map[string]any{"start": "0"}, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Domains))
	for _, d := range out.Domains {
		names = append(names, d.Domain)
	}
	return names, nil
}

// GetNameservers returns the authoritative nameservers for a domain.
func (c *Client) GetNameservers(ctx context.Context, domain string) ([]string, error) {
	var out struct {
		NS []string `json:"ns"`
	}
	if err := c.do(ctx, "/domain/getNs/"+domain, nil, &out); err != nil {
		return nil, err
	}
	return out.NS, nil
}

// SetNameservers points a domain at the given nameservers (use Porkbun's own to
// manage DNS through this API).
func (c *Client) SetNameservers(ctx context.Context, domain string, ns []string) error {
	return c.do(ctx, "/domain/updateNs/"+domain, map[string]any{"ns": ns}, nil)
}

// DNSRecord is a single DNS record as returned by /dns/retrieve.
type DNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"` // fully-qualified host
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     string `json:"ttl"`
	Prio    string `json:"prio"`
}

// RetrieveDNS returns all DNS records for a domain.
func (c *Client) RetrieveDNS(ctx context.Context, domain string) ([]DNSRecord, error) {
	var out struct {
		Records []DNSRecord `json:"records"`
	}
	if err := c.do(ctx, "/dns/retrieve/"+domain, nil, &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}

// DNSRecordInput describes a record to create or edit. Name is the subdomain
// only ("" for the root, "_dmarc" for _dmarc.<domain>).
type DNSRecordInput struct {
	Name    string // subdomain, "" for root
	Type    string // A, MX, TXT, CNAME, ...
	Content string
	TTL     int // seconds, defaults to 600
	Prio    int // for MX
}

// CreateDNS creates a DNS record and returns its id.
func (c *Client) CreateDNS(ctx context.Context, domain string, rec DNSRecordInput) (string, error) {
	body := dnsBody(rec)
	var out struct {
		ID json.Number `json:"id"`
	}
	if err := c.do(ctx, "/dns/create/"+domain, body, &out); err != nil {
		return "", err
	}
	return out.ID.String(), nil
}

// EditDNS replaces an existing record (by id) with the supplied values.
func (c *Client) EditDNS(ctx context.Context, domain, id string, rec DNSRecordInput) error {
	return c.do(ctx, "/dns/edit/"+domain+"/"+id, dnsBody(rec), nil)
}

func dnsBody(rec DNSRecordInput) map[string]any {
	ttl := rec.TTL
	if ttl <= 0 {
		ttl = 600
	}
	body := map[string]any{
		"name":    rec.Name,
		"type":    rec.Type,
		"content": rec.Content,
		"ttl":     fmt.Sprintf("%d", ttl),
	}
	if rec.Type == "MX" {
		body["prio"] = fmt.Sprintf("%d", rec.Prio)
	}
	return body
}

func boolToYN(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
