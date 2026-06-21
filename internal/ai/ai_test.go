package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zainclaude/goutreach/internal/store"
)

func TestStripEmDashes(t *testing.T) {
	cases := map[string]string{
		"We saw Acme — it's growing": "We saw Acme, it's growing",
		"fast—growing brand":         "fast, growing brand",
		"a – b":                      "a, b",
		"co-founder of well-being":   "co-founder of well-being", // plain hyphens kept
		"no dashes here":             "no dashes here",
	}
	for in, want := range cases {
		if got := stripEmDashes(in); got != want {
			t.Errorf("stripEmDashes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokenizeThenPersonalize(t *testing.T) {
	jane := store.Lead{FirstName: "Jane", LastName: "Doe", Email: "jane@acme.com"}
	body := "Hi Jane, saw Acme is crushing it. — Sam"
	tok := TokenizeName(body, jane)
	if !strings.Contains(tok, "Hi {{first_name}},") {
		t.Fatalf("tokenize didn't replace first name: %q", tok)
	}
	// Reuse the cached email for a different contact at the same brand.
	bob := store.Lead{FirstName: "Bob", Email: "bob@acme.com"}
	out := Personalize(tok, bob)
	if !strings.Contains(out, "Hi Bob,") {
		t.Fatalf("personalize didn't fill new name: %q", out)
	}
	if strings.Contains(out, "Jane") {
		t.Fatalf("old name leaked into reused email: %q", out)
	}
	if !strings.Contains(out, "Acme") {
		t.Fatalf("brand text should be preserved: %q", out)
	}
}

func TestRenderVars(t *testing.T) {
	lead := store.Lead{
		FirstName: "Jane", LastName: "Doe", Company: "Acme", Title: "CMO",
		Email: "jane@acme.com", CustomFields: json.RawMessage(`{"city":"Austin"}`),
	}
	cases := map[string]string{
		"Hi {{first_name}},":            "Hi Jane,",
		"saw {{brand_name}} is growing": "saw Acme is growing",
		"{{full_name}} ({{title}})":     "Jane Doe (CMO)",
		"in {{custom.city}}?":           "in Austin?",
		"by {{city}}":                   "by Austin",
		"{{ first_name }} spaced":       "Jane spaced",
		"{{unknown_token}} stays":       "{{unknown_token}} stays",
		"{{COMPANY}} caps-insensitive":  "Acme caps-insensitive",
	}
	for in, want := range cases {
		if got := renderVars(in, lead, lead.Company, true); got != want {
			t.Errorf("renderVars(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`{"a":1}`:                       `{"a":1}`,
		"prefix {\"a\":\"x\"} suffix":   `{"a":"x"}`,
		`{"s":"has } brace in string"}`: `{"s":"has } brace in string"}`,
		"text with no json":             ``,
		"```json\n{\"k\":\"v\"}\n```":   `{"k":"v"}`,
		`{"nested":{"x":1},"y":2}`:      `{"nested":{"x":1},"y":2}`,
	}
	for in, want := range cases {
		if got := extractJSON(in); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseResult(t *testing.T) {
	r, err := parseResult(`Here you go: {"template_used":"B","subject":"Hi","body":"Hello there","reasoning":"on amazon"}`)
	if err != nil {
		t.Fatalf("parseResult: %v", err)
	}
	if r.TemplateUsed != "B" || r.Subject != "Hi" || r.Body != "Hello there" {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestParseResultRejectsEmpty(t *testing.T) {
	if _, err := parseResult(`{"template_used":"A","subject":"","body":""}`); err == nil {
		t.Fatal("expected error for empty subject/body")
	}
}

func TestDomainOf(t *testing.T) {
	if got := domainOf("john@acme.com"); got != "acme.com" {
		t.Errorf("domainOf = %q", got)
	}
}
