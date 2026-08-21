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

The input is the NEW text the prospect wrote. Our outreach emails end with a
call to action like "just reply or say 'send' and I'll shoot you the link" —
so an extremely short affirmative reply ("send", "yes", "sure", "send it",
"ok", "interested") is a direct response to that CTA and is ALWAYS
"interested", no matter how terse.

Categories:
- "interested": a HUMAN engages with the offer itself — wants to learn more, asks questions about the offer or pricing, suggests a call, personally refers you to the right person for THIS offer, or answers the email's call to action (e.g. replies "send").
- "not_interested": explicitly declines or says it's not a fit (but doesn't demand removal).
- "ooo": ANY automated or templated reply — out-of-office, vacation, auto-acknowledgements, and announcements like "I've changed roles/companies", "you can now reach me at my new email", "I'm no longer with X", or "your message has been received". These are broadcast to every sender; boilerplate courtesy phrases in them ("feel free to reach out!", "don't hesitate to contact me", "still happy to collaborate") do NOT make them interested.
- "unsubscribe": asks to be removed, to stop emailing, or threatens spam complaints/legal action.
- "other": anything else (unclear, neutral, wrong person with no referral).

First decide: is this an automated/templated reply or a human who read the email? Automated replies are never "interested" no matter how friendly their wording. A human reply may quote our original email below their text — judge ONLY what the human wrote, never the quoted outreach. Only when a genuine human response is ambiguous between interested and other, prefer "interested" so a warm lead is never missed.`

var categoryRe = regexp.MustCompile(`\{[^{}]*"category"[^{}]*\}`)

// quoteMarkers begin the quoted/forwarded portion of a reply body. Text after
// the first marker is our own email echoed back — classifying it drowns a
// short human reply (e.g. the single word "send") in templated-looking text.
var quoteMarkers = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\s*>`),                              // "> quoted line"
	regexp.MustCompile(`(?mi)^On .{4,80} wrote:\s*$`),            // "On Fri, Aug 21... wrote:"
	regexp.MustCompile(`(?mi)^-{2,}\s*Original Message\s*-{2,}`), // Outlook
	regexp.MustCompile(`(?m)^_{10,}\s*$`),                        // Outlook separator
	regexp.MustCompile(`(?mi)^From:\s.+$`),                       // bare header block
	regexp.MustCompile(`(?mi)^-{2,}\s*Forwarded message\s*-{2,}`),
}

// StripQuoted returns only the new text of a reply, cutting the quoted thread
// below it. Falls back to the input when stripping would leave nothing.
func StripQuoted(body string) string {
	cut := len(body)
	for _, re := range quoteMarkers {
		if loc := re.FindStringIndex(body); loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	head := strings.TrimSpace(body[:cut])
	if head == "" {
		return strings.TrimSpace(body)
	}
	return head
}

// ClassifyReply categorizes an inbound reply. Uses a small fast model — the
// task is trivial and runs on every reply.
func (g *Generator) ClassifyReply(ctx context.Context, subject, body string) (string, error) {
	if !g.enabled {
		return "", errors.New("ai disabled")
	}
	body = StripQuoted(body)
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
