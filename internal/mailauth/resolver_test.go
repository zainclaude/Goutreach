package mailauth

import (
	"testing"

	"github.com/zainclaude/goutreach/internal/store"
)

func TestIMAPEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		acc      store.EmailAccount
		wantHost string
		wantPort int
	}{
		{"gmail wrong host gets corrected",
			store.EmailAccount{SMTPHost: "smtp.gmail.com", IMAPHost: "imap.growsocialcommerce.com", IMAPPort: 993},
			"imap.gmail.com", 993},
		{"gmail already correct",
			store.EmailAccount{SMTPHost: "smtp.gmail.com", IMAPHost: "imap.gmail.com", IMAPPort: 993},
			"imap.gmail.com", 993},
		{"office365 corrected",
			store.EmailAccount{SMTPHost: "smtp.office365.com", IMAPHost: "mail.brand.com", IMAPPort: 993},
			"outlook.office365.com", 993},
		{"custom provider left untouched",
			store.EmailAccount{SMTPHost: "smtp.mailgun.org", IMAPHost: "imap.mailgun.org", IMAPPort: 993},
			"imap.mailgun.org", 993},
		{"zero port defaults to 993",
			store.EmailAccount{SMTPHost: "smtp.custom.com", IMAPHost: "imap.custom.com", IMAPPort: 0},
			"imap.custom.com", 993},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, port := imapEndpoint(tc.acc)
			if host != tc.wantHost || port != tc.wantPort {
				t.Errorf("imapEndpoint() = %s:%d, want %s:%d", host, port, tc.wantHost, tc.wantPort)
			}
		})
	}
}
