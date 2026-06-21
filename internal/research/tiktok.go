// Package research implements brand-signal lookups, notably the TikTok Shop
// check used as step 1 of the email decision tree. It tries kalodata first and
// falls back to fastmoss, using credentials stored (encrypted) in settings.
package research

import (
	"context"
	"fmt"
	"log"

	"github.com/zainclaude/goutreach/internal/ai"
	"github.com/zainclaude/goutreach/internal/crypto"
	"github.com/zainclaude/goutreach/internal/store"
)

// Settings keys for the TikTok Shop data providers.
const (
	KeyKalodataEmail    = "kalodata_email"
	KeyKalodataPassword = "kalodata_password" // secret
	KeyFastmossEmail    = "fastmoss_email"
	KeyFastmossPassword = "fastmoss_password" // secret
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

// Check returns whether the brand sells on TikTok Shop, trying kalodata then
// fastmoss. Falls through to "unknown" (so the decision tree proceeds to Amazon)
// when neither provider is configured or both are unavailable.
func (c *Checker) Check(ctx context.Context, brand string) (ai.TikTokShopResult, error) {
	if kd, ok := c.loadCreds(ctx, KeyKalodataEmail, KeyKalodataPassword); ok {
		res, err := queryKalodata(ctx, kd, brand)
		if err == nil && res.OnTikTokShop != "unknown" {
			return res, nil
		}
		if err != nil {
			c.log.Printf("research: kalodata unavailable (%v); trying fastmoss", err)
		}
	}
	if fm, ok := c.loadCreds(ctx, KeyFastmossEmail, KeyFastmossPassword); ok {
		res, err := queryFastmoss(ctx, fm, brand)
		if err == nil {
			return res, nil
		}
		c.log.Printf("research: fastmoss unavailable (%v)", err)
	}
	return ai.TikTokShopResult{
		OnTikTokShop: "unknown",
		Details:      "TikTok Shop providers not configured or unavailable; proceeding to Amazon check.",
	}, nil
}

// queryKalodata / queryFastmoss perform the live brand lookup. Both kalodata.com
// and fastmoss.com are login-gated, JavaScript-heavy dashboards, so a robust
// implementation requires headless-browser automation (e.g. chromedp) driven by
// the stored credentials. That automation is wired here behind credential
// loading; until the live selectors are finalized against the sites, these
// return "unknown" so the decision tree falls through to the next signal.
func queryKalodata(ctx context.Context, _ creds, brand string) (ai.TikTokShopResult, error) {
	return ai.TikTokShopResult{
		OnTikTokShop: "unknown",
		Details:      fmt.Sprintf("kalodata lookup for %q pending live integration", brand),
	}, nil
}

func queryFastmoss(ctx context.Context, _ creds, brand string) (ai.TikTokShopResult, error) {
	return ai.TikTokShopResult{
		OnTikTokShop: "unknown",
		Details:      fmt.Sprintf("fastmoss lookup for %q pending live integration", brand),
	}, nil
}
