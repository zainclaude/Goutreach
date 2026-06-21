package sender

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

var urlRe = regexp.MustCompile(`https?://[^\s<>"')]+`)

// buildHTML converts a plain-text body to HTML, optionally wrapping links for
// click tracking and appending a 1x1 open-tracking pixel.
func buildHTML(plain, appURL string, messageID int64, trackOpens, trackClicks bool) string {
	escaped := html.EscapeString(plain)

	if trackClicks {
		escaped = urlRe.ReplaceAllStringFunc(escaped, func(u string) string {
			// u is HTML-escaped already; unescape for the redirect target.
			target := html.UnescapeString(u)
			click := fmt.Sprintf("%s/t/click/%d?u=%s", appURL, messageID, url.QueryEscape(target))
			return fmt.Sprintf(`<a href="%s">%s</a>`, click, u)
		})
	}

	body := strings.ReplaceAll(escaped, "\n", "<br>\n")
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><body style="font-family:Arial,Helvetica,sans-serif;font-size:14px;color:#222;">`)
	sb.WriteString(body)
	if trackOpens {
		fmt.Fprintf(&sb, `<img src="%s/t/open/%d.png" width="1" height="1" style="display:none" alt="">`, appURL, messageID)
	}
	sb.WriteString(`</body></html>`)
	return sb.String()
}
