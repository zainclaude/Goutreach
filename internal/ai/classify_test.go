package ai

import "testing"

// The "send" case: a one-word human reply above the quoted original email
// (with From:/To: headers) must survive quote-stripping intact.
func TestStripQuoted(t *testing.T) {
	jackie := "send\n\nFrom: Zain Ali <zainali@startsocialcommerce.com>\nTo: \"Jackie Mao\"<jackie@iconthin.com>\nDate: Fri, 21 Aug 2026 11:24:40 -0400\nSubject: Iconthin Biotech Amazon Question\n\nHi Jackie,\n\nI've been following Iconthin Biotech..."
	if got := StripQuoted(jackie); got != "send" {
		t.Errorf("jackie case: got %q, want %q", got, "send")
	}

	gmail := "Sounds interesting, can you call Tuesday?\n\nOn Fri, Aug 21, 2026 at 11:24 AM Zain Ali wrote:\n> Hi Jackie,\n> I've been following..."
	if got := StripQuoted(gmail); got != "Sounds interesting, can you call Tuesday?" {
		t.Errorf("gmail case: got %q", got)
	}

	outlook := "Not a fit for us right now.\n\n________________________________\nFrom: Zain Ali\nSent: Friday"
	if got := StripQuoted(outlook); got != "Not a fit for us right now." {
		t.Errorf("outlook case: got %q", got)
	}

	// A body that is ONLY quoted content falls back to the full text.
	onlyQuote := "> just quoted stuff\n> nothing new"
	if got := StripQuoted(onlyQuote); got == "" {
		t.Errorf("only-quote case returned empty")
	}

	// No markers at all: unchanged.
	plain := "Happy to chat, send the loom over."
	if got := StripQuoted(plain); got != plain {
		t.Errorf("plain case: got %q", got)
	}
}
