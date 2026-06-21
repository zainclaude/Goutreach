// Package ai runs the brand-research decision tree and writes a personalized
// cold email for a lead using Claude with web search/fetch tools.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/zainclaude/goutreach/internal/store"
)

// Generator produces personalized cold emails via Claude.
type Generator struct {
	client  anthropic.Client
	enabled bool
	tiktok  TikTokShopChecker
}

// New builds a Generator. If apiKey is empty the generator is disabled and
// Generate returns a clear error (so the rest of the app still runs).
func New(apiKey string, tiktok TikTokShopChecker) *Generator {
	g := &Generator{tiktok: tiktok}
	if tiktok == nil {
		g.tiktok = UnconfiguredTikTok{}
	}
	if apiKey != "" {
		g.client = anthropic.NewClient(option.WithAPIKey(apiKey))
		g.enabled = true
	}
	return g
}

// Enabled reports whether an API key is configured.
func (g *Generator) Enabled() bool { return g.enabled }

// Input bundles everything needed to write one email.
type Input struct {
	Brief     string
	Angle     string // per-step instruction (e.g. follow-up)
	Lead      store.Lead
	Templates []store.EmailTemplate
	FromName  string
}

// Result is the structured output of a generation run.
type Result struct {
	TemplateUsed string `json:"template_used"`
	Subject      string `json:"subject"`
	Body         string `json:"body"`
	Reasoning    string `json:"reasoning"`
}

var errDisabled = errors.New("ai generation disabled: ANTHROPIC_API_KEY not set")

// markerFillRules tells the model how to resolve the (A#n)/(C#n) placeholders the
// templates carry. The cardinal rule is verify-or-omit: every figure must come
// from tool research, and any figure that cannot be verified is dropped (its
// parenthetical or whole line removed) — never fabricated.
const markerFillRules = `
TEMPLATE MARKER FILL-IN RULES:
The chosen template's text contains parenthetical placeholders tagged with codes
like (A#0), (A#1) ... (C#0). Replace each placeholder using the rule below and
remove the tag itself from the final copy. Base every number ONLY on data you
verified with the tools. If a specific figure cannot be verified, apply its
fallback — do NOT invent numbers. Boundary values go to the higher tier.

Template A (TikTok Shop) — metrics come from Kalodata-style TikTok Shop data:
- A#0  Performance phrase from monthly TikTok Shop revenue:
       >= $100K/mo -> "crushing it"; $20K-$100K/mo -> "picking up";
       < $20K/mo -> "just getting started".
       Fallback (revenue unverifiable): pick the phrase that best fits the
       strongest public signals, and do NOT state a dollar figure you didn't verify.
- A#1  Number of active affiliates -> place inside the parenthesis.
       Fallback: if unverifiable, remove that parenthetical entirely.
- A#2  Number of videos posted in the last 30 days -> place inside the parenthesis.
       Fallback: if unverifiable, remove that parenthetical entirely.
- A#3  Video-volume nudge, from the A#2 number:
       < 300 -> "at least 300 videos/month";
       300-1,000 -> "at least 1,000 videos/month";
       1,000-2,000 -> "at least 2,000 videos/month";
       >= 2,000 -> delete this line entirely.
       Fallback: if A#2 is unverifiable, delete this line.
- A#4  Sales-lift percentage tied to A#0:
       "crushing it" -> 20%; "picking up" -> 50%; "just getting started" -> 100%.

Template C (Meta ads):
- C#0  Number of ACTIVE ads in the United States from the Meta Ad Library ->
       place inside the parenthesis. This count also qualifies the brand for
       Template C. Fallback: if you cannot determine the count, do NOT use
       Template C — continue the decision tree to the next step.
`

// Generate runs the research decision tree and writes the email.
func (g *Generator) Generate(ctx context.Context, in Input) (Result, error) {
	if !g.enabled {
		return Result{}, errDisabled
	}

	brand := in.Lead.Company
	if brand == "" {
		brand = domainOf(in.Lead.Email)
	}

	tools := []anthropic.ToolUnionParam{
		{OfWebSearchTool20260209: &anthropic.WebSearchTool20260209Param{}},
		{OfWebFetchTool20260209: &anthropic.WebFetchTool20260209Param{}},
		{OfTool: &anthropic.ToolParam{
			Name:        "check_tiktok_shop",
			Description: anthropic.String("Check whether a brand sells on TikTok Shop using the kalodata.com data source. Returns on_tiktok_shop = yes | no | unknown."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"brand_name": map[string]any{
						"type":        "string",
						"description": "The brand/company name to look up.",
					},
				},
				Required: []string{"brand_name"},
			},
		}},
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeOpus4_8,
		MaxTokens: 8000,
		System: []anthropic.TextBlockParam{{
			Text: g.systemPrompt(in, brand),
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(
				fmt.Sprintf("Research the brand %q and write the email now. Follow the decision tree, then output ONLY the final JSON object.", brand))),
		},
		Tools: tools,
	}

	const maxIters = 14
	for i := 0; i < maxIters; i++ {
		resp, err := g.client.Messages.New(ctx, params)
		if err != nil {
			return Result{}, fmt.Errorf("claude: %w", err)
		}
		params.Messages = append(params.Messages, resp.ToParam())

		switch resp.StopReason {
		case anthropic.StopReasonPauseTurn:
			// Server-side tool loop paused; resend to continue.
			continue
		case anthropic.StopReasonToolUse:
			results, err := g.runCustomTools(ctx, brand, resp)
			if err != nil {
				return Result{}, err
			}
			if len(results) == 0 {
				// No custom tool to run but model stopped on tool_use — resend.
				continue
			}
			params.Messages = append(params.Messages, anthropic.NewUserMessage(results...))
			continue
		default:
			return parseResult(collectText(resp))
		}
	}
	return Result{}, errors.New("generation did not converge within iteration limit")
}

// runCustomTools executes any check_tiktok_shop tool calls and returns tool results.
func (g *Generator) runCustomTools(ctx context.Context, brand string, resp *anthropic.Message) ([]anthropic.ContentBlockParamUnion, error) {
	var results []anthropic.ContentBlockParamUnion
	for _, block := range resp.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok || tu.Name != "check_tiktok_shop" {
			continue
		}
		var args struct {
			BrandName string `json:"brand_name"`
		}
		_ = json.Unmarshal([]byte(tu.JSON.Input.Raw()), &args)
		q := args.BrandName
		if q == "" {
			q = brand
		}
		res, err := g.tiktok.Check(ctx, q)
		if err != nil {
			results = append(results, anthropic.NewToolResultBlock(tu.ID,
				fmt.Sprintf(`{"on_tiktok_shop":"unknown","details":%q}`, "check failed: "+err.Error()), false))
			continue
		}
		out, _ := json.Marshal(res)
		results = append(results, anthropic.NewToolResultBlock(tu.ID, string(out), false))
	}
	return results, nil
}

func (g *Generator) systemPrompt(in Input, brand string) string {
	var b strings.Builder
	b.WriteString("You are an expert B2B cold-email copywriter for a marketing agency. ")
	b.WriteString("You research a prospect's brand and write a single, highly personalized cold email.\n\n")

	b.WriteString("LEAD / BRAND:\n")
	fmt.Fprintf(&b, "- Brand/company: %s\n", brand)
	if n := strings.TrimSpace(in.Lead.FirstName + " " + in.Lead.LastName); n != "" {
		fmt.Fprintf(&b, "- Contact name: %s\n", n)
	}
	if in.Lead.Title != "" {
		fmt.Fprintf(&b, "- Contact title: %s\n", in.Lead.Title)
	}
	fmt.Fprintf(&b, "- Contact email: %s\n", in.Lead.Email)
	if len(in.Lead.CustomFields) > 0 && string(in.Lead.CustomFields) != "{}" {
		fmt.Fprintf(&b, "- Extra fields: %s\n", string(in.Lead.CustomFields))
	}
	fmt.Fprintf(&b, "- Sender name (sign-off): %s\n\n", in.FromName)

	if in.Brief != "" {
		b.WriteString("CAMPAIGN BRIEF (offer, ICP, tone):\n")
		b.WriteString(in.Brief + "\n\n")
	}
	if in.Angle != "" {
		b.WriteString("STEP ANGLE (this specific email):\n")
		b.WriteString(in.Angle + "\n\n")
	}

	b.WriteString(`DECISION TREE — follow IN ORDER to select exactly one template:
Step 1. Determine whether the brand sells on TikTok Shop. Call check_tiktok_shop
        first; if it returns "unknown", fall back to web_search/web_fetch (search
        "<brand> TikTok Shop", check tiktok.com and public kalodata.com pages).
        If there is credible evidence the brand sells on TikTok Shop -> TEMPLATE A.
        Otherwise continue.
Step 2. Use web_search/web_fetch to check amazon.com for the brand's products.
        If the brand sells on Amazon -> use TEMPLATE B. Otherwise continue.
Step 3. Check the Meta Ad Library for ACTIVE ads in the United States by fetching
        https://www.facebook.com/ads/library/?active_status=active&ad_type=all&country=US&media_type=all&search_type=keyword_unordered&q=<brand>
        (URL-encode the brand name). Count the active ads — this is C#0.
        If there is at least 1 active US ad -> use TEMPLATE C. Otherwise continue.
Step 4. Use web_search to assess whether the brand has a big retail presence
        (sold in major retailers like Target, Walmart, Sephora, Ulta, etc.).
        If yes -> use TEMPLATE D. If no -> use the generic TEMPLATE E.

`)

	b.WriteString("TEMPLATES (merge variables like {{first_name}} have already been filled in from this lead's data; personalize the chosen one for this specific brand and contact; keep the template's structure and intent, fill in researched specifics, never invent facts you did not verify):\n\n")
	for _, t := range in.Templates {
		fmt.Fprintf(&b, "TEMPLATE %s — %s\n", t.Key, firstNonEmpty(t.Name, store.TemplateDefaults[t.Key]))
		subject := renderVars(t.Subject, in.Lead, brand)
		body := renderVars(t.Body, in.Lead, brand)
		if strings.TrimSpace(subject) == "" && strings.TrimSpace(body) == "" {
			b.WriteString("(No template text provided yet — write a sensible, concise cold email that fits this template's intent described above.)\n\n")
			continue
		}
		if subject != "" {
			fmt.Fprintf(&b, "Subject: %s\n", subject)
		}
		fmt.Fprintf(&b, "Body:\n%s\n\n", body)
	}

	b.WriteString(markerFillRules)

	b.WriteString(`OUTPUT RULES:
- Do your research with the tools first.
- Then respond with ONLY a single JSON object, no prose around it:
  {"template_used":"A|B|C|D|E","subject":"...","body":"...","reasoning":"one or two sentences on why this template and what you personalized"}
- The body should be plain text (you may use \n for line breaks), ready to send, signed off as the sender.
- Keep it concise and human. No placeholders like [Name] — fill everything in.`)

	return b.String()
}

// --- helpers ---

func collectText(resp *anthropic.Message) string {
	var sb strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

func parseResult(text string) (Result, error) {
	js := extractJSON(text)
	if js == "" {
		return Result{}, fmt.Errorf("no JSON object in model output: %.200s", text)
	}
	var r Result
	if err := json.Unmarshal([]byte(js), &r); err != nil {
		return Result{}, fmt.Errorf("parse result JSON: %w", err)
	}
	if strings.TrimSpace(r.Subject) == "" || strings.TrimSpace(r.Body) == "" {
		return Result{}, errors.New("model returned empty subject or body")
	}
	return r, nil
}

// extractJSON returns the first balanced {...} object found in s.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

var varRe = regexp.MustCompile(`\{\{\s*[a-zA-Z0-9_.]+\s*\}\}`)

// renderVars replaces {{variable}} merge tokens in a template with values from
// the lead. Supported: first_name, last_name, full_name, company, brand_name/
// brand, title, email, and custom.<key> (or a bare <key>) for CSV custom fields.
// Unknown tokens are left untouched. Matching is case-insensitive.
func renderVars(s string, lead store.Lead, brand string) string {
	vars := map[string]string{
		"first_name": lead.FirstName,
		"last_name":  lead.LastName,
		"full_name":  strings.TrimSpace(lead.FirstName + " " + lead.LastName),
		"name":       strings.TrimSpace(lead.FirstName + " " + lead.LastName),
		"company":    lead.Company,
		"brand_name": brand,
		"brand":      brand,
		"title":      lead.Title,
		"email":      lead.Email,
	}
	var custom map[string]any
	if len(lead.CustomFields) > 0 {
		_ = json.Unmarshal(lead.CustomFields, &custom)
	}
	lookupCustom := func(key string) (string, bool) {
		for k, v := range custom {
			if strings.EqualFold(k, key) {
				return fmt.Sprint(v), true
			}
		}
		return "", false
	}
	return varRe.ReplaceAllStringFunc(s, func(m string) string {
		key := strings.ToLower(strings.Trim(m, "{} \t"))
		if strings.HasPrefix(key, "custom.") {
			if v, ok := lookupCustom(key[len("custom."):]); ok {
				return v
			}
			return ""
		}
		if v, ok := vars[key]; ok {
			return v
		}
		if v, ok := lookupCustom(key); ok {
			return v
		}
		return m // leave unknown tokens as-is
	})
}

func domainOf(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 {
		return email[i+1:]
	}
	return email
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
