package ai

import "testing"

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
