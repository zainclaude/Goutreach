package mailer

import "testing"

func TestParseMessagePlain(t *testing.T) {
	raw := "From: Lead <lead@brand.com>\r\n" +
		"Subject: Re: Hi\r\n" +
		"References: <abc@x.com>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Sure, send it over.\r\n\r\n" +
		"On Tue, Jun 24 2026, Zain wrote:\r\n" +
		"> original line\r\n"
	refs, text := parseMessage([]byte(raw))
	if refs != "<abc@x.com>" {
		t.Errorf("references = %q", refs)
	}
	if text != "Sure, send it over." {
		t.Errorf("text = %q, want quoted history trimmed", text)
	}
}

func TestParseMessageHTML(t *testing.T) {
	raw := "From: Lead <lead@brand.com>\r\n" +
		"Subject: Re: Hi\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<div>Yes please &amp; thanks</div><div>Talk soon</div>\r\n"
	_, text := parseMessage([]byte(raw))
	if text != "Yes please & thanks\nTalk soon" {
		t.Errorf("html text = %q", text)
	}
}
