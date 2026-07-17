// Package beehiiv adds subscribers to a beehiiv newsletter publication.
package beehiiv

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

const baseURL = "https://api.beehiiv.com/v2"

// Subscribe upserts an email as a subscriber of the publication. beehiiv treats
// re-subscribing an existing address as a no-op, so this is safe to call more
// than once for the same lead.
func Subscribe(ctx context.Context, apiKey, publicationID, email string) error {
	payload, _ := json.Marshal(map[string]any{
		"email":      strings.ToLower(strings.TrimSpace(email)),
		"reinvite":   false,
		"utm_source": "pipelinebuilder",
		"utm_medium": "cold_outreach_reply",
	})
	url := fmt.Sprintf("%s/publications/%s/subscriptions", baseURL, strings.TrimSpace(publicationID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
	return fmt.Errorf("beehiiv: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
