// Package fastmoss is a client for the FastMoss OpenAPI (TikTok Shop data),
// used as the primary source for brand metrics: monthly GMV, the number of
// creators/affiliates a shop works with, and the number of videos for the shop.
// Auth is a client_secret sent as a Bearer token. See developers.fastmoss.com.
package fastmoss

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://openapi.fastmoss.com"

// Endpoint paths (from the FastMoss pricing/reference). Shop request/response
// field names below follow FastMoss's documented convention; if a future doc
// revision renames a field, adjust the structs in BrandMetrics.
const (
	pathShopSearch  = "/shop/v1/search"
	pathShopCreator = "/shop/v1/creatorList"
	pathShopVideo   = "/shop/v1/videoList"
)

// Client talks to the FastMoss OpenAPI with a bearer client_secret.
type Client struct {
	secret  string
	baseURL string
	http    *http.Client
	// Log, if set, receives per-call diagnostics (which shop matched, the totals
	// returned, and any errors) so shop-field issues are visible in server logs.
	Log *log.Logger
}

func (c *Client) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log.Printf(format, args...)
	}
}

// New builds a client. baseURL may be empty to use DefaultBaseURL.
func New(secret, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{secret: secret, baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

// Configured reports whether a client_secret is present.
func (c *Client) Configured() bool { return c.secret != "" }

// envelope is FastMoss's standard wrapper: {code, msg, data, request_id, timestamp}.
type envelope struct {
	Code    int             `json:"code"`
	Msg     string          `json:"msg"`
	Message string          `json:"message"` // some endpoints use "message" instead of "msg"
	Data    json.RawMessage `json:"data"`
}

// errText returns the server's error text wherever it was put.
func (e envelope) errText(raw []byte) string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Message != "" {
		return e.Message
	}
	return "body: " + snippet(raw)
}

// post sends a JSON body and decodes the envelope's data into out.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.secret)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: http %d: %s", path, resp.StatusCode, snippet(raw))
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s: decode: %w", path, err)
	}
	if env.Code != 0 {
		return fmt.Errorf("%s: code %d: %s", path, env.Code, env.errText(raw))
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("%s: decode data: %w", path, err)
		}
	}
	return nil
}

// Metrics holds the figures the A#0–A#2 markers need for a brand.
type Metrics struct {
	Found      bool
	ShopName   string
	RevenueUSD float64
	Creators   int // shop's cooperating creators/affiliates in the period
	Videos     int // shop's videos in the period
}

// VerifyKey checks the client_secret works by issuing a tiny shop search.
// The search term goes in the top-level "keywords" field (filter has no
// "keyword" key), and pagesize must be in [10,100].
func (c *Client) VerifyKey(ctx context.Context) error {
	var out json.RawMessage
	return c.post(ctx, pathShopSearch, map[string]any{
		"keywords": "nike", "filter": map[string]any{"region": "US"}, "page": 1, "pagesize": 10,
	}, &out)
}

// BrandMetrics resolves a brand to a shop and returns its GMV + creator/video
// counts for the trailing month.
func (c *Client) BrandMetrics(ctx context.Context, brand string) (Metrics, error) {
	// FastMoss keyword search often misses on long multi-word names even when
	// the shop is indexed ("HealthForce SuperFoods" -> 0 hits, "HealthForce" ->
	// hit). Try progressively simpler keywords; pickShop still enforces a name
	// match against the full brand, so simpler keywords can't grab a wrong shop.
	var shop map[string]any
	for _, kw := range searchKeywords(brand) {
		var search struct {
			Total int              `json:"total"`
			List  []map[string]any `json:"list"`
		}
		// The search term is the top-level "keywords" (fuzzy match); filter.keyword
		// is not a real field, so sending it there returns an unfiltered default
		// list. Order by total GMV so the largest matching shop surfaces first.
		if err := c.post(ctx, pathShopSearch, map[string]any{
			"keywords": kw,
			"filter":   map[string]any{"region": "US"},
			"orderby":  []map[string]any{{"field": "total_gmv", "order": "desc"}},
			"page":     1, "pagesize": 10,
		}, &search); err != nil {
			return Metrics{}, err
		}
		c.logf("fastmoss: search %q returned %d shops; keys of top hit: %v", kw, len(search.List), keysOf(search.List))
		if len(search.List) > 0 {
			if b, err := json.Marshal(search.List[0]); err == nil {
				c.logf("fastmoss: top result raw: %s", clip(string(b), 700))
			}
		}
		if shop = pickShop(brand, search.List); shop != nil {
			break
		}
		c.logf("fastmoss: no shop matched %q via keyword %q", brand, kw)
	}
	if shop == nil {
		return Metrics{}, nil // not found on TikTok Shop
	}
	shopID := firstStr(shop, "seller_id", "shop_id", "id")
	if shopID == "" {
		return Metrics{}, nil
	}

	m := Metrics{
		Found:      true,
		ShopName:   firstStr(shop, nameKeys...),
		RevenueUSD: firstNum(shop, "total_gmv", "usd_gmv", "shop_usd_gmv", "gmv", "revenue"),
		// Affiliate/creator count comes back on the shop search result itself.
		Creators: int(firstNum(shop, "affiliate_creator_count", "creator_count", "creator_num")),
	}
	c.logf("fastmoss: matched shop=%s name=%q gmv=%.0f affiliates=%d", shopID, m.ShopName, m.RevenueUSD, m.Creators)
	// If the search didn't carry an affiliate count, fall back to the list endpoint.
	if m.Creators == 0 {
		m.Creators = c.shopTotal(ctx, pathShopCreator, map[string]any{"seller_id": shopID})
	}
	// Videos the shop posted in the trailing 30 days. videoList has no date_info
	// field — the time window is create_time_range in unix seconds.
	now := time.Now().Unix()
	m.Videos = c.shopTotal(ctx, pathShopVideo, map[string]any{
		"seller_id":         shopID,
		"create_time_range": map[string]any{"min": now - 30*24*3600, "max": now},
	})
	return m, nil
}

// nameKeys are the candidate fields a shop's display name may appear under.
var nameKeys = []string{"brand", "shop_name", "name", "creator_name", "title"}

// shopTotal returns data.total for a shop-scoped list endpoint (creators/videos)
// using the supplied filter. Returns 0 on error (metric simply omitted upstream);
// the call + result + any error is logged so a wrong field is diagnosable.
func (c *Client) shopTotal(ctx context.Context, path string, filter map[string]any) int {
	var out struct {
		Total json.RawMessage `json:"total"` // number on some endpoints, quoted string on others
	}
	body := map[string]any{
		"filter": filter,
		"page":   1, "pagesize": 10, // pagesize must be in [10,100]; we only read data.total
	}
	err := c.post(ctx, path, body, &out)
	total := flexInt(out.Total)
	c.logf("fastmoss: %s filter=%v -> total=%d err=%v", path, filter, total, err)
	return total
}

// flexInt parses an integer the API may return as either a JSON number (shop
// search) or a quoted string (videoList's data.total).
func flexInt(raw json.RawMessage) int {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if s == "" || s == "null" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

// keysOf lists the field names of the first result, to reveal the actual shop
// schema (so we can confirm the id/gmv field names).
func keysOf(list []map[string]any) []string {
	if len(list) == 0 {
		return nil
	}
	var ks []string
	for k := range list[0] {
		ks = append(ks, k)
	}
	return ks
}

// pickShop chooses the best brand-name match from search results, preferring an
// exact normalized name, then a strict directional match, to avoid grabbing the
// wrong shop.
func pickShop(brand string, list []map[string]any) map[string]any {
	want := NormBrand(brand)
	if want == "" || len(list) == 0 {
		return nil
	}
	// Exact normalized name first (best precision), checking every result.
	for _, s := range list {
		if normName(firstStr(s, nameKeys...)) == want {
			return s
		}
	}
	// Otherwise the top (most keyword-relevant) hit if its name credibly
	// belongs to the brand — handles "Mary Ruth's" vs "MaryRuth Organics".
	top := list[0]
	if NameMatches(want, normName(firstStr(top, nameKeys...))) {
		return top
	}
	return nil
}

// NameMatches reports whether a (normalized) shop name credibly belongs to the
// (normalized) brand name. Direction matters: a shop that EXTENDS the brand
// name ("Nike" -> "Nike Official Store", "BISSELL" -> "BISSELL Clean") is
// credible, but a shop that is only a FRAGMENT of the brand ("Broken Arrow"
// for "Broken Arrow Electric Supply") is usually a different company sharing a
// generic prefix — those are rejected so no metrics get attributed to the
// wrong shop. A false negative just routes the lead to the next template; a
// false positive writes someone else's revenue into the email.
func NameMatches(want, got string) bool {
	if want == "" || got == "" {
		return false
	}
	if want == got {
		return true
	}
	// Whole brand name contained in the shop name (shop = brand + qualifiers).
	if len(want) >= 4 && strings.Contains(got, want) {
		return true
	}
	// Shop name shares a prefix covering (nearly) the whole brand name —
	// tolerates spelling drift at the end ("maryruths" vs "maryruthorganics").
	cp := 0
	for cp < len(want) && cp < len(got) && want[cp] == got[cp] {
		cp++
	}
	return cp >= 5 && cp >= len(want)-2
}

// NormBrand normalizes a brand name for matching, dropping trailing legal
// suffixes ("Inc", "LLC", ...) that shop names rarely carry.
func NormBrand(brand string) string {
	fields := strings.Fields(brand)
	for len(fields) > 1 && legalSuffixes[strings.Trim(strings.ToLower(fields[len(fields)-1]), ".,")] {
		fields = fields[:len(fields)-1]
	}
	return normName(strings.Join(fields, " "))
}

func normName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if t != "" {
					return t
				}
			case float64:
				return fmt.Sprintf("%.0f", t)
			case json.Number:
				return t.String()
			}
		}
	}
	return ""
}

func firstNum(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case float64:
				return t
			case json.Number:
				f, _ := t.Float64()
				return f
			case string:
				var f float64
				if _, err := fmt.Sscanf(strings.ReplaceAll(strings.TrimPrefix(t, "$"), ",", ""), "%g", &f); err == nil {
					return f
				}
			}
		}
	}
	return 0
}

func snippet(b []byte) string {
	return clip(strings.TrimSpace(string(b)), 300)
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// legalSuffixes are trailing company-name tokens that hurt keyword recall.
var legalSuffixes = map[string]bool{
	"inc": true, "inc.": true, "llc": true, "ltd": true, "ltd.": true,
	"corp": true, "corp.": true, "corporation": true, "co": true, "co.": true,
	"company": true, "gmbh": true, "limited": true,
}

// searchKeywords returns the keyword ladder for a brand: the full name, the
// name without legal suffixes, and the first word — deduped, in order.
func searchKeywords(brand string) []string {
	out := []string{brand}
	seen := map[string]bool{strings.ToLower(brand): true}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	fields := strings.Fields(brand)
	for len(fields) > 1 && legalSuffixes[strings.ToLower(fields[len(fields)-1])] {
		fields = fields[:len(fields)-1]
	}
	add(strings.Join(fields, " "))
	if len(fields) > 1 && len(fields[0]) >= 4 {
		add(fields[0])
	}
	return out
}
