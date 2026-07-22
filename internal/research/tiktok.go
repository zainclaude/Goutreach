// Package research implements brand-signal lookups, notably the TikTok Shop
// check used as step 1 of the email decision tree. It logs into kalodata.com with
// the user's stored credentials and reads the brand's shop metrics (revenue,
// affiliates, videos), falling back to fastmoss when kalodata is unavailable.
package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"time"

	"github.com/zainclaude/goutreach/internal/ai"
	"github.com/zainclaude/goutreach/internal/crypto"
	"github.com/zainclaude/goutreach/internal/fastmoss"
	"github.com/zainclaude/goutreach/internal/store"
)

// Settings keys for the TikTok Shop data providers.
const (
	KeyFastmossClientSecret = "fastmoss_client_secret" // secret — FastMoss OpenAPI (primary)
	KeyKalodataEmail        = "kalodata_email"
	KeyKalodataPassword     = "kalodata_password" // secret
	KeyFastmossEmail        = "fastmoss_email"
	KeyFastmossPassword     = "fastmoss_password" // secret
)

// Checker implements ai.TikTokShopChecker using kalodata with a fastmoss fallback.
type Checker struct {
	st     *store.Store
	cipher *crypto.Cipher
	log    *log.Logger
}

// New builds a Checker. The owning user is resolved lazily (single-tenant MVP).
func New(st *store.Store, cipher *crypto.Cipher, logger *log.Logger) *Checker {
	return &Checker{st: st, cipher: cipher, log: logger}
}

type creds struct {
	email, password string
}

func (c *Checker) loadCreds(ctx context.Context, emailKey, passKey string) (creds, bool) {
	userID, ok := c.st.FirstUserID(ctx)
	if !ok {
		return creds{}, false
	}
	email, ok, _ := c.st.GetSetting(ctx, userID, emailKey)
	if !ok || email == "" {
		return creds{}, false
	}
	encPass, ok, _ := c.st.GetSetting(ctx, userID, passKey)
	if !ok || encPass == "" {
		return creds{}, false
	}
	pass, err := c.cipher.Decrypt(encPass)
	if err != nil {
		return creds{}, false
	}
	return creds{email: email, password: pass}, true
}

// loadSecret returns a single decrypted secret setting (e.g. an API token).
func (c *Checker) loadSecret(ctx context.Context, key string) (string, bool) {
	userID, ok := c.st.FirstUserID(ctx)
	if !ok {
		return "", false
	}
	enc, ok, _ := c.st.GetSetting(ctx, userID, key)
	if !ok || enc == "" {
		return "", false
	}
	v, err := c.cipher.Decrypt(enc)
	if err != nil || v == "" {
		return "", false
	}
	return v, true
}

// Check returns the brand's TikTok Shop status and metrics. It uses the FastMoss
// OpenAPI as the primary source and falls back to scraping kalodata. Falls through
// to "unknown" (so the decision tree proceeds to Amazon) when no provider is
// configured or all are unavailable.
func (c *Checker) Check(ctx context.Context, brand string) (ai.TikTokShopResult, error) {
	if secret, ok := c.loadSecret(ctx, KeyFastmossClientSecret); ok {
		res, err := queryFastmossAPI(ctx, c.log, secret, brand)
		if err == nil && res.OnTikTokShop != "unknown" {
			return res, nil
		}
		if err != nil {
			c.log.Printf("research: fastmoss unavailable (%v); trying kalodata", err)
		}
	}
	if kd, ok := c.loadCreds(ctx, KeyKalodataEmail, KeyKalodataPassword); ok {
		res, err := queryKalodata(ctx, kd, brand)
		if err == nil && res.OnTikTokShop != "unknown" {
			return res, nil
		}
		if err != nil {
			c.log.Printf("research: kalodata unavailable (%v)", err)
		}
	}
	return ai.TikTokShopResult{
		OnTikTokShop: "unknown",
		Details:      "TikTok Shop providers not configured or unavailable; proceeding to Amazon check.",
	}, nil
}

// --- kalodata client ---

const kalodataBase = "https://www.kalodata.com"

// kdClient is a cookie-jar HTTP client for kalodata.com. Auth is carried entirely
// in cookies set by /user/login, so subsequent calls just reuse the jar.
type kdClient struct {
	http *http.Client
}

func newKDClient() (*kdClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &kdClient{http: &http.Client{Timeout: 30 * time.Second, Jar: jar}}, nil
}

// kdEnvelope is kalodata's standard response wrapper: {success, data, message,...}.
type kdEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Message *string         `json:"message"`
}

// post sends a JSON body and unmarshals the envelope's data into out.
func (c *kdClient) post(ctx context.Context, path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kalodataBase+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/plain, */*")
	req.Header.Set("country", "US")
	req.Header.Set("currency", "USD")
	req.Header.Set("language", "en-US")
	req.Header.Set("origin", kalodataBase)
	req.Header.Set("referer", kalodataBase+"/")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		// 403 here almost always means Cloudflare blocked the server-side request.
		return fmt.Errorf("%s: http %d", path, resp.StatusCode)
	}
	var env kdEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s: decode envelope: %w", path, err)
	}
	if !env.Success {
		msg := ""
		if env.Message != nil {
			msg = *env.Message
		}
		return fmt.Errorf("%s: not success: %s", path, msg)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("%s: decode data: %w", path, err)
		}
	}
	return nil
}

// login authenticates; the session is stored in the cookie jar.
func (c *kdClient) login(ctx context.Context, email, password string) error {
	return c.post(ctx, "/user/login", map[string]any{
		"scene":         "login",
		"loginMethod":   "EMAIL_PASSWORD",
		"tcCode":        "",
		"email":         email,
		"emailPassword": password,
	}, nil)
}

// searchShopID maps a brand name to its kalodata shop id via the full-text
// search endpoint, which returns sellers ranked by relevance.
func (c *kdClient) searchShopID(ctx context.Context, brand string) (string, error) {
	body := map[string]any{
		"country_code": "us",
		"keyword":      brand,
		"scope":        []any{map[string]any{"index": "seller", "pageNo": 1, "pageSize": 10}},
	}
	var out struct {
		Seller []sellerHit `json:"seller"`
	}
	if err := c.post(ctx, "/overview/fullText/search", body, &out); err != nil {
		return "", err
	}
	return pickSellerID(brand, out.Seller), nil
}

// sellerHit is one match from /overview/fullText/search (already score-sorted).
type sellerHit struct {
	SellerID   string  `json:"seller_id"`
	SellerName string  `json:"seller_name"`
	GMVin30    float64 `json:"gmv_in_30"`
	Score      float64 `json:"score"`
}

// pickSellerID chooses the right seller for a brand name. It prefers an exact
// normalized name match (and since hits are score-sorted, the first such match is
// the most relevant — e.g. the real "MaryRuth's" over a $0 duplicate), then falls
// back to the top hit only when its name credibly belongs to the brand (see
// fastmoss.NameMatches — a shop that's just a fragment of the brand name is
// rejected). No confident match returns "" so the caller omits the markers
// rather than use the wrong shop.
func pickSellerID(brand string, hits []sellerHit) string {
	want := fastmoss.NormBrand(brand)
	if want == "" {
		return ""
	}
	for _, s := range hits {
		if normName(s.SellerName) == want {
			return s.SellerID
		}
	}
	if len(hits) > 0 {
		if fastmoss.NameMatches(want, normName(hits[0].SellerName)) {
			return hits[0].SellerID
		}
	}
	return ""
}

// normName lowercases and strips everything but letters/digits for fuzzy matching.
func normName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// kdMetrics holds the three figures the A#* markers need.
type kdMetrics struct {
	RevenueUSD float64
	Affiliates int
	Videos30d  int
}

// shopMetrics reads trailing-30-day revenue, active affiliate count, and 30-day
// video count for a shop id.
func (c *kdClient) shopMetrics(ctx context.Context, id string) (kdMetrics, error) {
	var m kdMetrics
	start, end := last30()
	base := func() map[string]any {
		return map[string]any{"id": id, "startDate": start, "endDate": end, "cateIds": []any{}, "authority": true}
	}

	// Revenue (raw float).
	totalBody := base()
	totalBody["historyStartDate"] = start
	totalBody["historyEndDate"] = end
	var total struct {
		OriginalRevenue float64 `json:"original_revenue"`
	}
	if err := c.post(ctx, "/shop/detail/total", totalBody, &total); err != nil {
		return m, err
	}
	m.RevenueUSD = total.OriginalRevenue

	// Active affiliates (raw int returned directly as data).
	cntBody := base()
	cntBody["pageNo"] = 1
	cntBody["pageSize"] = 10
	cntBody["sort"] = []any{map[string]any{"field": "revenue", "type": "DESC"}}
	cntBody["creatorType"] = ""
	if err := c.post(ctx, "/shop/detail/searchCooperativeCreators/count", cntBody, &m.Affiliates); err != nil {
		return m, err
	}

	// Videos in the last 30 days (formatted like "17.12k").
	extraBody := base()
	extraBody["historyStartDate"] = start
	extraBody["historyEndDate"] = end
	var extra struct {
		AffiliateNewVideoCount string `json:"affiliate_new_video_count"`
	}
	if err := c.post(ctx, "/shop/detail/extraTotal", extraBody, &extra); err != nil {
		return m, err
	}
	m.Videos30d = parseKM(extra.AffiliateNewVideoCount)

	return m, nil
}

// queryKalodata logs in, resolves the brand's shop, and reads its metrics.
func queryKalodata(ctx context.Context, cr creds, brand string) (ai.TikTokShopResult, error) {
	unknown := ai.TikTokShopResult{
		OnTikTokShop: "unknown",
		Details:      fmt.Sprintf("kalodata: could not resolve %q", brand),
	}
	c, err := newKDClient()
	if err != nil {
		return unknown, err
	}
	if err := c.login(ctx, cr.email, cr.password); err != nil {
		return unknown, fmt.Errorf("login: %w", err)
	}
	id, err := c.searchShopID(ctx, brand)
	if err != nil {
		return unknown, err
	}
	if id == "" {
		return unknown, nil // not found (or search endpoint not yet wired)
	}
	m, err := c.shopMetrics(ctx, id)
	if err != nil {
		return unknown, err
	}
	// kalodata's index carries $0 stale/duplicate seller entries. A shop with
	// zero GMV, zero affiliates, and zero videos is indistinguishable from a
	// dead index entry — don't claim the brand sells on TikTok Shop from it;
	// let the decision tree keep going (web search may still find evidence).
	if m.RevenueUSD <= 0 && m.Affiliates == 0 && m.Videos30d == 0 {
		return ai.TikTokShopResult{
			OnTikTokShop: "unknown",
			Details:      fmt.Sprintf("kalodata: found a %q entry but with zero GMV/affiliates/videos — likely a stale index entry, not a live shop; treat as unverified", brand),
		}, nil
	}
	res := ai.TikTokShopResult{
		OnTikTokShop: "yes",
		Details:      fmt.Sprintf("kalodata: %s — %s", brand, metricSummary(m.RevenueUSD, m.Affiliates, m.Videos30d)),
	}
	// Individual zero metrics mean "unavailable", not a verified zero — omit
	// them (same rule as the fastmoss path) so the generator drops the marker
	// instead of writing "you have 0 affiliates" into the email.
	if m.RevenueUSD > 0 {
		res.MonthlyRevenueUSD = &m.RevenueUSD
	}
	if m.Affiliates > 0 {
		res.ActiveAffiliates = &m.Affiliates
	}
	if m.Videos30d > 0 {
		res.Videos30d = &m.Videos30d
	}
	return res, nil
}

// metricSummary describes only the verified (non-zero) figures, naming the
// missing ones as unverified — the model must never see a literal zero it
// could quote into the email ("you posted 0 videos").
func metricSummary(rev float64, affiliates, videos int) string {
	var have, missing []string
	if rev > 0 {
		have = append(have, fmt.Sprintf("monthly GMV $%.0f", rev))
	} else {
		missing = append(missing, "GMV")
	}
	if affiliates > 0 {
		have = append(have, fmt.Sprintf("%d active affiliates", affiliates))
	} else {
		missing = append(missing, "affiliate count")
	}
	if videos > 0 {
		have = append(have, fmt.Sprintf("%d videos in the last 30 days", videos))
	} else {
		missing = append(missing, "video count")
	}
	s := strings.Join(have, ", ")
	if len(missing) > 0 {
		s += " (" + strings.Join(missing, ", ") + " unverified — omit those markers)"
	}
	return s
}

// last30 returns the trailing 30-day window (yesterday back 30 days) as YYYY-MM-DD,
// matching how the kalodata dashboard requests shop metrics.
func last30() (string, string) {
	end := time.Now().UTC().AddDate(0, 0, -1)
	start := end.AddDate(0, 0, -29)
	const f = "2006-01-02"
	return start.Format(f), end.Format(f)
}

// parseKM converts kalodata's human-formatted numbers ("17.12k", "$3.95m",
// "6.28k", "570", "1,234") into an int. Unparseable input yields 0.
func parseKM(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1e3, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult, s = 1e6, strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "b"):
		mult, s = 1e9, strings.TrimSuffix(s, "b")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int(f * mult)
}

// queryFastmossAPI is the primary provider: the FastMoss OpenAPI. It resolves the
// brand to a shop and returns GMV + creator/video counts.
func queryFastmossAPI(ctx context.Context, logger *log.Logger, secret, brand string) (ai.TikTokShopResult, error) {
	unknown := ai.TikTokShopResult{
		OnTikTokShop: "unknown",
		Details:      fmt.Sprintf("fastmoss: could not resolve %q", brand),
	}
	fc := fastmoss.New(secret, "")
	fc.Log = logger
	m, err := fc.BrandMetrics(ctx, brand)
	if err != nil {
		return unknown, err
	}
	if !m.Found {
		return unknown, nil
	}
	// Same dead-entry guard as kalodata: a matched shop with zero GMV, zero
	// creators, and zero videos proves nothing — treat as unresolved.
	if m.RevenueUSD <= 0 && m.Creators == 0 && m.Videos == 0 {
		return ai.TikTokShopResult{
			OnTikTokShop: "unknown",
			Details:      fmt.Sprintf("fastmoss: found a %q entry but with zero GMV/creators/videos — likely a stale index entry, not a live shop; treat as unverified", brand),
		}, nil
	}
	rev, creators, videos := m.RevenueUSD, m.Creators, m.Videos
	res := ai.TikTokShopResult{
		OnTikTokShop: "yes",
		Details:      fmt.Sprintf("fastmoss: %s — %s", brand, metricSummary(rev, creators, videos)),
	}
	// Only surface metrics that came back non-zero. A 0 here means the value was
	// unavailable (e.g. an endpoint returned nothing), not a real zero, so leave
	// it nil and let the generator omit that marker rather than writing "0".
	if rev > 0 {
		res.MonthlyRevenueUSD = &rev
	}
	if creators > 0 {
		res.ActiveAffiliates = &creators
	}
	if videos > 0 {
		res.Videos30d = &videos
	}
	return res, nil
}
