package ai

import "context"

// TikTokShopResult is what a TikTok Shop check returns.
type TikTokShopResult struct {
	OnTikTokShop string `json:"on_tiktok_shop"` // "yes" | "no" | "unknown"
	Details      string `json:"details"`
}

// TikTokShopChecker determines whether a brand sells on TikTok Shop. The
// production implementation logs into kalodata.com with the user's credentials
// and searches the brand; until those credentials/automation are wired up, the
// Unconfigured checker returns "unknown" so the decision tree falls through to
// the next step (Amazon).
type TikTokShopChecker interface {
	Check(ctx context.Context, brand string) (TikTokShopResult, error)
}

// UnconfiguredTikTok returns "unknown" for every brand. It is the default until
// kalodata credentials and the scraping integration are provided.
type UnconfiguredTikTok struct{}

func (UnconfiguredTikTok) Check(ctx context.Context, brand string) (TikTokShopResult, error) {
	return TikTokShopResult{
		OnTikTokShop: "unknown",
		Details:      "kalodata integration not configured; TikTok Shop status could not be verified — proceed to the next step (Amazon).",
	}, nil
}
