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
	"net/http"
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
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
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
		return fmt.Errorf("%s: code %d: %s", path, env.Code, env.Msg)
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
func (c *Client) VerifyKey(ctx context.Context) error {
	var out json.RawMessage
	return c.post(ctx, pathShopSearch, map[string]any{
		"filter": map[string]any{"region": "US", "keyword": "nike"}, "page": 1, "pagesize": 1,
	}, &out)
}

// BrandMetrics resolves a brand to a shop and returns its GMV + creator/video
// counts for the trailing month.
func (c *Client) BrandMetrics(ctx context.Context, brand string) (Metrics, error) {
	month := time.Now().UTC().Format("2006-01")

	var search struct {
		Total int              `json:"total"`
		List  []map[string]any `json:"list"`
	}
	if err := c.post(ctx, pathShopSearch, map[string]any{
		"filter": map[string]any{"region": "US", "keyword": brand}, "page": 1, "pagesize": 10,
	}, &search); err != nil {
		return Metrics{}, err
	}
	shop := pickShop(brand, search.List)
	if shop == nil {
		return Metrics{}, nil // not found on TikTok Shop
	}
	shopID := firstStr(shop, "shop_id", "id", "seller_id")
	if shopID == "" {
		return Metrics{}, nil
	}

	m := Metrics{
		Found:      true,
		ShopName:   firstStr(shop, "shop_name", "name", "title"),
		RevenueUSD: firstNum(shop, "usd_gmv", "shop_usd_gmv", "gmv", "revenue", "total_gmv"),
	}
	m.Creators = c.shopTotal(ctx, pathShopCreator, shopID, month)
	m.Videos = c.shopTotal(ctx, pathShopVideo, shopID, month)
	return m, nil
}

// shopTotal returns data.total for a shop-scoped list endpoint (creators/videos)
// over the given month. Returns 0 on error (metric simply omitted upstream).
func (c *Client) shopTotal(ctx context.Context, path, shopID, month string) int {
	var out struct {
		Total int `json:"total"`
	}
	body := map[string]any{
		"filter": map[string]any{
			"shop_id":   shopID,
			"date_info": map[string]any{"type": "month", "value": month},
		},
		"page": 1, "pagesize": 1,
	}
	if err := c.post(ctx, path, body, &out); err != nil {
		return 0
	}
	return out.Total
}

// pickShop chooses the best brand-name match from search results, preferring an
// exact normalized name, then a containment match, to avoid grabbing the wrong shop.
func pickShop(brand string, list []map[string]any) map[string]any {
	want := normName(brand)
	if want == "" || len(list) == 0 {
		return nil
	}
	for _, s := range list {
		if normName(firstStr(s, "shop_name", "name", "title")) == want {
			return s
		}
	}
	top := list[0]
	tn := normName(firstStr(top, "shop_name", "name", "title"))
	if tn != "" && (strings.Contains(tn, want) || strings.Contains(want, tn)) {
		return top
	}
	return nil
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
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
