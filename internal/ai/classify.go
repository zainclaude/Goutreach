package ai

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Reply categories assigned by ClassifyReply.
var replyCategories = map[string]bool{
	"interested": true, "not_interested": true, "ooo": true, "unsubscribe": true, "other": true,
}

const classifySystem = `You classify replies to cold outreach emails. Respond with ONLY a JSON object: {"category":"<one>"}.

Categories:
- "interested": positive engagement — wants to learn more, asks questions about the offer or pricing, suggests a call, or refers you to the right person.
- "not_interested": explicitly declines or says it's not a fit (but doesn't demand removal).
- "ooo": automated out-of-office, vacation, or auto-acknowledgement replies.
- "unsubscribe": asks to be removed, to stop emailing, or threatens spam complaints/legal action.
- "other": anything else (unclear, neutral, wrong person with no referral).

When in doubt between interested and other, prefer "interested" so a warm lead is never missed.`

var categoryRe = regexp.MustCompile(`\{[^{}]*"category"[^{}]*\}`)

// ClassifyReply categorizes an inbound reply. Uses a small fast model — the
// task is trivial and runs on every reply.
func (g *Generator) ClassifyReply(ctx context.Context, subject, body string) (string, error) {
	if !g.enabled {
		return "", errors.New("ai disabled")
	}
	if len(body) > 4000 {
		body = body[:4000]
	}
	resp, err := g.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model("claude-haiku-4-5"),
		MaxTokens: 100,
		System:    []anthropic.TextBlockParam{{Text: classifySystem}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Subject: " + subject + "\n\n" + body)),
		},
	})
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for _, block := range resp.Content {
		if b, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	m := categoryRe.FindString(text.String())
	if m == "" {
		return "other", nil
	}
	var out struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal([]byte(m), &out); err != nil || !replyCategories[out.Category] {
		return "other", nil
	}
	return out.Category, nil
}
