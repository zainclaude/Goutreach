// Package ai runs the brand-research decision tree and writes a personalized
// cold email for a lead using Claude with web search/fetch tools.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
Step 1. Use the check_tiktok_shop tool to see if the brand is on TikTok Shop.
        If on_tiktok_shop = "yes" -> use TEMPLATE A. Otherwise continue.
Step 2. Use web_search/web_fetch to check amazon.com for the brand's products.
        If the brand sells on Amazon -> use TEMPLATE B. Otherwise continue.
Step 3. Use web_search/web_fetch to check the Meta Ad Library
        (facebook.com/ads/library) for active ads from the brand.
        If the brand is running Meta ads -> use TEMPLATE C. Otherwise continue.
Step 4. Use web_search to assess whether the brand has a big retail presence
        (sold in major retailers like Target, Walmart, Sephora, Ulta, etc.).
        If yes -> use TEMPLATE D. If no -> use the generic TEMPLATE E.

`)

	b.WriteString("TEMPLATES (personalize the chosen one for this specific brand and contact; keep the template's structure and intent, fill in researched specifics, never invent facts you did not verify):\n\n")
	for _, t := range in.Templates {
		fmt.Fprintf(&b, "TEMPLATE %s — %s\n", t.Key, firstNonEmpty(t.Name, store.TemplateDefaults[t.Key]))
		if strings.TrimSpace(t.Subject) == "" && strings.TrimSpace(t.Body) == "" {
			b.WriteString("(No template text provided yet — write a sensible, concise cold email that fits this template's intent described above.)\n\n")
			continue
		}
		if t.Subject != "" {
			fmt.Fprintf(&b, "Subject: %s\n", t.Subject)
		}
		fmt.Fprintf(&b, "Body:\n%s\n\n", t.Body)
	}

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
