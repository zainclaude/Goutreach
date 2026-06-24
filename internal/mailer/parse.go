package mailer

import (
	"bytes"
	"html"
	"io"
	"regexp"
	"strings"

	"github.com/emersion/go-message/mail"

	// Register decoders for non-UTF-8 charsets so CreateReader can read them.
	_ "github.com/emersion/go-message/charset"
)

// parseMessage parses a raw RFC822 message and returns its References header and
// a best-effort plaintext body (HTML converted to text, quoted history trimmed).
func parseMessage(raw []byte) (references, text string) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return "", ""
	}
	references = strings.TrimSpace(mr.Header.Get("References"))

	var plain, htmlBody string
	for {
		part, err := mr.NextPart()
		if err != nil {
			break // io.EOF or a malformed part — use whatever we have
		}
		ih, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue // attachment
		}
		ct, _, _ := ih.ContentType()
		b, _ := io.ReadAll(part.Body)
		switch {
		case strings.HasPrefix(ct, "text/plain") && plain == "":
			plain = string(b)
		case strings.HasPrefix(ct, "text/html") && htmlBody == "":
			htmlBody = string(b)
		}
	}

	body := plain
	if strings.TrimSpace(body) == "" {
		body = htmlToText(htmlBody)
	}
	return references, trimQuoted(body)
}

var (
	tagRe     = regexp.MustCompile(`(?s)<(script|style)\b.*?</\s*(script|style)\s*>`)
	anyTagRe  = regexp.MustCompile(`(?s)<[^>]+>`)
	blankRe   = regexp.MustCompile(`\n[ \t]*\n([ \t]*\n)+`)
	quoteLine = regexp.MustCompile(`(?m)^\s*>`)
)

// htmlToText strips HTML to a rough plaintext approximation.
func htmlToText(s string) string {
	if s == "" {
		return ""
	}
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = regexp.MustCompile(`(?i)</(p|div|tr|li|h[1-6])>`).ReplaceAllString(s, "\n")
	s = anyTagRe.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

// trimQuoted drops the quoted previous message (the "On … wrote:" history,
// signatures-before-quotes aside) so we show the lead's actual new reply, and
// caps the length. Best-effort — if no marker is found the whole body is kept.
func trimQuoted(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	markers := []*regexp.Regexp{
		regexp.MustCompile(`(?im)^On .+ wrote:\s*$`),
		regexp.MustCompile(`(?im)^-{2,}\s*Original Message\s*-{2,}`),
		regexp.MustCompile(`(?im)^_{5,}\s*$`),
		regexp.MustCompile(`(?im)^From:\s.+\n(Sent|Date):\s`),
	}
	cut := len(s)
	for _, re := range markers {
		if loc := re.FindStringIndex(s); loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	s = s[:cut]
	// Drop trailing ">"-quoted lines if the whole tail is a quote block.
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && quoteLine.MatchString(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	s = strings.Join(lines, "\n")
	s = blankRe.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		s = s[:4000] + "…"
	}
	return s
}
