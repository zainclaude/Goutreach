// Package ai runs the brand-research decision tree and writes a personalized
// cold email for a lead using Claude with web search/fetch tools.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/zainclaude/goutreach/internal/search"
	"github.com/zainclaude/goutreach/internal/store"
)

// Generator produces personalized cold emails via Claude (default) or an
// alternate provider like Kimi (per-user setting; see provider.go).
type Generator struct {
	client  anthropic.Client
	enabled bool
	model   anthropic.Model
	tiktok  TikTokShopChecker
	log     *log.Logger

	// Wired by ConfigureProviders — settings lookup for alternate providers.
	settings SettingsSource
	dec      Decrypter
}

// New builds a Generator. If apiKey is empty the generator is disabled and
// Generate returns a clear error (so the rest of the app still runs).
func New(apiKey, model string, tiktok TikTokShopChecker, logger *log.Logger) *Generator {
	if model == "" {
		model = "claude-sonnet-5"
	}
	g := &Generator{model: anthropic.Model(model), tiktok: tiktok, log: logger}
	if g.log == nil {
		g.log = log.New(io.Discard, "", 0)
	}
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
	Provider  string // "" or "claude" (default) | "kimi"
}

// Result is the structured output of a generation run.
type Result struct {
	TemplateUsed string `json:"template_used"`
	Subject      string `json:"subject"`
	Body         string `json:"body"`
	Reasoning    string `json:"reasoning"`
	Provider     string `json:"provider"` // which backend generated it (for A/B provenance)
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

Template A (TikTok Shop) — the figures come from the check_tiktok_shop tool
(monthly_revenue_usd, active_affiliates, videos_last_30d); any field the tool
omits is unverified, so apply that marker's fallback:
- A#0  Performance phrase from monthly TikTok Shop revenue:
       >= $100K/mo -> "crushing it"; $20K-$100K/mo -> "picking up";
       < $20K/mo -> "just getting started".
       Fallback (revenue unverifiable): pick the phrase that best fits the
       strongest public signals, and do NOT state a dollar figure you didn't verify.
- A#1  Number of active affiliates -> place inside the parenthesis.
       Fallback: if unverifiable OR the value is 0, remove that parenthetical
       entirely — never write a zero into the email.
- A#2  Number of videos posted in the last 30 days -> place inside the parenthesis.
       Fallback: if unverifiable OR the value is 0, remove that parenthetical
       entirely — never write a zero into the email.
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
	run, err := g.providerFor(ctx, in)
	if err != nil {
		return Result{}, err
	}

	brand := strings.TrimSpace(in.Lead.Company)
	brandIsDomain := false
	if brand == "" {
		brand = domainOf(in.Lead.Email)
		brandIsDomain = true
	}
	g.log.Printf("ai[%s] start provider=%s model=%s (brand_from_domain=%v)", brand, run.name, run.model, brandIsDomain)

	var tools []anthropic.ToolUnionParam
	if run.serperKey != "" {
		// Provider without a server-side search tool (Kimi): client-side
		// Serper-backed web_search, executed in runCustomTools.
		tools = append(tools, clientWebSearchTool())
	} else {
		// Use the 2025-03-05 web_search: it returns plain web_search_tool_result
		// blocks that round-trip through ToParam correctly. The 2026-02-09 variant
		// runs server-side in a code-execution container and returns
		// code_execution_tool_result blocks whose error variant the SDK fails to
		// re-serialize, 400-ing every multi-turn continuation.
		tools = append(tools, anthropic.ToolUnionParam{OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{}})
	}
	tools = append(tools, anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        "check_tiktok_shop",
			Description: anthropic.String("Check whether a brand sells on TikTok Shop via our TikTok Shop analytics providers. Returns on_tiktok_shop = yes | no | unknown, and — when available — the brand's metrics: monthly_revenue_usd (trailing-30d TikTok Shop GMV), active_affiliates, and videos_last_30d. Use these for the A#0–A#4 markers. A missing metric field means it is unverified — omit that marker, never state or imply a number for it (and never write a zero). Do not name the data provider in the email or reasoning."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"brand_name": map[string]any{
						"type":        "string",
						"description": "The brand/company name to look up.",
					},
				},
				Required: []string{"brand_name"},
			},
		},
	})

	params := anthropic.MessageNewParams{
		Model:     run.model,
		MaxTokens: 12000,
		System: []anthropic.TextBlockParam{{
			Text: g.systemPrompt(in, brand, brandIsDomain),
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(kickoff(brand, brandIsDomain))),
		},
		Tools: tools,
	}

	// Claude's server-side web search runs many searches inside one iteration;
	// a client-side search tool (Kimi) burns one iteration per round-trip, so
	// it needs more room to walk the same decision tree.
	maxIters := 14
	if run.serperKey != "" {
		maxIters = 24
	}
	for i := 0; i < maxIters; i++ {
		resp, err := run.client.Messages.New(ctx, params)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", run.name, err)
		}
		params.Messages = append(params.Messages, resp.ToParam())
		g.logTurn(brand, i, resp)

		switch resp.StopReason {
		case anthropic.StopReasonPauseTurn:
			// Server-side tool loop paused; resend to continue.
			continue
		case anthropic.StopReasonToolUse:
			results, err := g.runCustomTools(ctx, brand, run, resp)
			if err != nil {
				return Result{}, err
			}
			if len(results) == 0 {
				// No custom tool to run but model stopped on tool_use — resend.
				continue
			}
			// Nearing the cap: tell the model to stop researching and finalize,
			// so runs converge with fallbacks instead of dying at the limit.
			if i >= maxIters-4 {
				results = append(results, anthropic.NewTextBlock(
					"NOTE: You are almost out of research turns. Do not call any more tools. "+
						"Finalize now: pick the template supported by what you have verified so far, "+
						"apply the fallback rules for anything unverified, and output ONLY the final JSON object."))
			}
			params.Messages = append(params.Messages, anthropic.NewUserMessage(results...))
			continue
		default:
			res, err := parseResult(collectText(resp))
			if err == nil {
				res.Subject = stripEmDashes(res.Subject)
				res.Body = stripEmDashes(res.Body)
				res.Provider = run.name
				g.log.Printf("ai[%s] ✅ DONE provider=%s template=%s — %s", brand, run.name, res.TemplateUsed, truncate(res.Reasoning, 300))
			}
			return res, err
		}
	}
	return Result{}, errors.New("generation did not converge within iteration limit")
}

// runCustomTools executes client-side tool calls (check_tiktok_shop, and — on
// providers without server-side search — web_search) and returns tool results.
func (g *Generator) runCustomTools(ctx context.Context, brand string, run providerRun, resp *anthropic.Message) ([]anthropic.ContentBlockParamUnion, error) {
	var results []anthropic.ContentBlockParamUnion
	for _, block := range resp.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}
		switch tu.Name {
		case "check_tiktok_shop":
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
				g.log.Printf("ai[%s] check_tiktok_shop(%q) error: %v", brand, q, err)
				results = append(results, anthropic.NewToolResultBlock(tu.ID,
					fmt.Sprintf(`{"on_tiktok_shop":"unknown","details":%q}`, "check failed: "+err.Error()), false))
				continue
			}
			out, _ := json.Marshal(res)
			g.log.Printf("ai[%s] check_tiktok_shop(%q) -> %s", brand, q, string(out))
			results = append(results, anthropic.NewToolResultBlock(tu.ID, string(out), false))

		case "web_search":
			if run.serperKey == "" {
				continue // Claude's web_search is server-side; nothing to do here
			}
			var args struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal([]byte(tu.JSON.Input.Raw()), &args)
			if strings.TrimSpace(args.Query) == "" {
				results = append(results, anthropic.NewToolResultBlock(tu.ID, "error: empty query", true))
				continue
			}
			out, err := search.Serper(ctx, run.serperKey, args.Query, 8)
			if err != nil {
				g.log.Printf("ai[%s] web_search(%q) error: %v", brand, args.Query, err)
				results = append(results, anthropic.NewToolResultBlock(tu.ID, "search failed: "+err.Error(), true))
				continue
			}
			g.log.Printf("ai[%s] web_search(%q) -> %d bytes", brand, args.Query, len(out))
			results = append(results, anthropic.NewToolResultBlock(tu.ID, out, false))
		}
	}
	return results, nil
}

func (g *Generator) systemPrompt(in Input, brand string, brandIsDomain bool) string {
	var b strings.Builder
	b.WriteString("You are an expert B2B cold-email copywriter for a marketing agency. ")
	b.WriteString("You research a prospect's brand and write a single, highly personalized cold email.\n\n")

	b.WriteString("LEAD / BRAND:\n")
	if brandIsDomain {
		fmt.Fprintf(&b, "- Website domain (NO clean company name was provided — derive the real brand name from this): %s\n", brand)
	} else {
		fmt.Fprintf(&b, "- Brand/company: %s\n", brand)
	}
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

	if brandIsDomain {
		fmt.Fprintf(&b, "BRAND NAME RESOLUTION (do this FIRST):\n"+
			"We only have the website domain %q, not a clean company name. Before any other research, "+
			"use web_search to determine the company's real, properly-capitalized brand name "+
			"(e.g. \"naturalfactors.com\" is the brand \"Natural Factors\"). Use that real name for every "+
			"tool call (including check_tiktok_shop) and everywhere in the subject and body. NEVER write a "+
			"bare domain or URL as if it were the brand name, and replace any remaining {{brand_name}} "+
			"placeholder with the real name.\n\n", brand)
	}

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
        first; if it returns "unknown", fall back to web_search (search
        "<brand> TikTok Shop", tiktok.com and public kalodata.com mentions).
        If there is credible evidence the brand sells on TikTok Shop -> TEMPLATE A.
        Otherwise continue.
Step 2. Use web_search to check whether the brand sells on amazon.com.
        If the brand sells on Amazon -> use TEMPLATE B. Otherwise continue.
Step 3. Use web_search to assess whether the brand runs ACTIVE ads in the US
        (search "<brand> Meta ad library", "<brand> facebook ads"). If you can
        verify a specific count of active US ads, that is C#0; if you cannot
        verify a number, omit the C#0 marker (never guess a count).
        If there is credible evidence of at least 1 active US ad -> use TEMPLATE C.
        Otherwise continue.
Step 4. Use web_search to assess whether the brand has a big retail presence
        (sold in major retailers like Target, Walmart, Sephora, Ulta, etc.).
        If yes -> use TEMPLATE D. If no -> use the generic TEMPLATE E.

`)

	b.WriteString("TEMPLATES (merge variables like {{first_name}} have already been filled in from this lead's data; personalize the chosen one for this specific brand and contact; keep the template's structure and intent, fill in researched specifics, never invent facts you did not verify):\n\n")
	for _, t := range in.Templates {
		fmt.Fprintf(&b, "TEMPLATE %s — %s\n", t.Key, firstNonEmpty(t.Name, store.TemplateDefaults[t.Key]))
		subject := renderVars(t.Subject, in.Lead, brand, !brandIsDomain)
		body := renderVars(t.Body, in.Lead, brand, !brandIsDomain)
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
- Keep it concise and human. No placeholders like [Name]; fill everything in.
- NEVER use em dashes (—) or en dashes (–) anywhere in the subject or body. They
  are a dead giveaway that a message was written by AI. Use commas, periods, or
  shorter sentences instead. Plain hyphens in normal words are fine.`)

	return b.String()
}

// --- helpers ---

// Personalize fills per-lead merge tokens (name, title, email, custom.*) into a
// cached brand email. Brand-level details were already baked in at generation time.
func Personalize(s string, lead store.Lead) string {
	return renderVars(s, lead, lead.Company, true)
}

// TokenizeName replaces the lead's name with merge tokens so a generated email
// can be cached at the brand level and re-personalized for other contacts. Longer
// (full) names are replaced first so "Jane Doe" doesn't leave a stray "Doe".
func TokenizeName(s string, lead store.Lead) string {
	repl := func(in, name, token string) string {
		if strings.TrimSpace(name) == "" {
			return in
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		return re.ReplaceAllString(in, token)
	}
	full := strings.TrimSpace(lead.FirstName + " " + lead.LastName)
	s = repl(s, full, "{{full_name}}")
	s = repl(s, lead.FirstName, "{{first_name}}")
	s = repl(s, lead.LastName, "{{last_name}}")
	return s
}

// kickoff is the first user message; it nudges domain-only leads to resolve the
// real brand name before researching.
func kickoff(brand string, brandIsDomain bool) string {
	if brandIsDomain {
		return fmt.Sprintf("We only have this lead's website domain: %q. First determine the company's real brand name, then follow the decision tree and write the email. Output ONLY the final JSON object.", brand)
	}
	return fmt.Sprintf("Research the brand %q and write the email now. Follow the decision tree, then output ONLY the final JSON object.", brand)
}

// logTurn writes a readable trace of one model turn: its visible reasoning text
// and any tool calls it made. This is what shows "how Claude is thinking" in the
// server logs during generation.
func (g *Generator) logTurn(brand string, iter int, resp *anthropic.Message) {
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			if t := strings.TrimSpace(b.Text); t != "" {
				g.log.Printf("ai[%s] i%d 💭 %s", brand, iter, truncate(t, 600))
			}
		case anthropic.ToolUseBlock:
			g.log.Printf("ai[%s] i%d 🔧 %s(%s)", brand, iter, b.Name, truncate(b.JSON.Input.Raw(), 300))
		}
	}
}

// stripEmDashes removes em/en dashes (a common AI tell) from final copy, replacing
// them with natural punctuation, in case the model uses one despite the prompt.
func stripEmDashes(s string) string {
	// Spaced dashes ("a — b") become a comma; unspaced ("a—b") too.
	for _, d := range []string{" — ", " – ", " —", " –", "— ", "– ", "—", "–"} {
		s = strings.ReplaceAll(s, d, ", ")
	}
	// Tidy artifacts from the replacement.
	s = strings.ReplaceAll(s, " ,", ",")
	s = strings.ReplaceAll(s, ",,", ",")
	s = strings.ReplaceAll(s, "  ", " ")
	return s
}

// truncate collapses whitespace and caps length for tidy single-line log entries.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

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
func renderVars(s string, lead store.Lead, brand string, brandKnown bool) string {
	vars := map[string]string{
		"first_name": lead.FirstName,
		"last_name":  lead.LastName,
		"full_name":  strings.TrimSpace(lead.FirstName + " " + lead.LastName),
		"name":       strings.TrimSpace(lead.FirstName + " " + lead.LastName),
		"title":      lead.Title,
		"email":      lead.Email,
	}
	// When we don't have a real brand name (only a domain), leave the brand
	// tokens unrendered so the model fills them with the name it researches,
	// rather than baking the raw domain into the copy.
	if brandKnown {
		vars["company"] = lead.Company
		vars["brand_name"] = brand
		vars["brand"] = brand
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
