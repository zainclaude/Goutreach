// Package mailer wraps SMTP sending and IMAP polling for connected mailboxes.
package mailer

import (
	"context"
	"fmt"
	"time"

	"github.com/wneessen/go-mail"
)

// SMTPCreds describes how to authenticate to a sending server. If OAuthToken is
// set, XOAUTH2 is used (Username is the mailbox address); otherwise Password auth.
type SMTPCreds struct {
	Host       string
	Port       int
	Username   string
	Password   string
	OAuthToken string
}

// OutgoingEmail is a message to send.
type OutgoingEmail struct {
	FromAddr  string
	FromName  string
	ToAddr    string
	ToName    string
	Cc        []string // optional carbon-copy recipients
	Subject   string
	HTMLBody  string
	TextBody  string
	MessageID string // value without angle brackets; if empty go-mail generates one
	InReplyTo string // full <...> value for threading follow-ups
	Refs      string // References header value
}

func newClient(c SMTPCreds) (*mail.Client, error) {
	opts := []mail.Option{
		mail.WithPort(c.Port),
		mail.WithUsername(c.Username),
		mail.WithTimeout(30 * time.Second),
	}
	if c.OAuthToken != "" {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthXOAUTH2), mail.WithPassword(c.OAuthToken))
	} else {
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthPlain), mail.WithPassword(c.Password))
	}
	if c.Port == 465 {
		opts = append(opts, mail.WithSSL())
	} else {
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	}
	return mail.NewClient(c.Host, opts...)
}

// VerifySMTP dials and authenticates without sending, to validate credentials.
// The dial is retried on transient network errors (some mail edges intermittently
// answer without a valid TLS handshake); auth failures are not retried.
func VerifySMTP(ctx context.Context, c SMTPCreds) error {
	var dialErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
		}
		client, err := newClient(c)
		if err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
		dialErr = client.DialWithContext(ctx)
		if dialErr == nil {
			return client.Close()
		}
		if !isTransientNet(dialErr) {
			break
		}
	}
	return fmt.Errorf("smtp dial/auth: %w", dialErr)
}

// Send delivers an email and returns the Message-ID value (without angle brackets) used.
func Send(ctx context.Context, c SMTPCreds, e OutgoingEmail) (string, error) {
	client, err := newClient(c)
	if err != nil {
		return "", fmt.Errorf("smtp client: %w", err)
	}

	msg := mail.NewMsg()
	if err := msg.FromFormat(e.FromName, e.FromAddr); err != nil {
		return "", fmt.Errorf("from: %w", err)
	}
	if err := msg.AddToFormat(e.ToName, e.ToAddr); err != nil {
		return "", fmt.Errorf("to: %w", err)
	}
	if len(e.Cc) > 0 {
		if err := msg.Cc(e.Cc...); err != nil {
			return "", fmt.Errorf("cc: %w", err)
		}
	}
	msg.Subject(e.Subject)

	if e.MessageID != "" {
		msg.SetMessageIDWithValue(e.MessageID)
	} else {
		msg.SetMessageID()
	}
	if e.InReplyTo != "" {
		msg.SetGenHeader(mail.HeaderInReplyTo, e.InReplyTo)
	}
	if e.Refs != "" {
		msg.SetGenHeader(mail.HeaderReferences, e.Refs)
	}

	if e.TextBody != "" {
		msg.SetBodyString(mail.TypeTextPlain, e.TextBody)
	}
	if e.HTMLBody != "" {
		if e.TextBody != "" {
			msg.AddAlternativeString(mail.TypeTextHTML, e.HTMLBody)
		} else {
			msg.SetBodyString(mail.TypeTextHTML, e.HTMLBody)
		}
	}

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return "", fmt.Errorf("send: %w", err)
	}
	return msg.GetMessageID(), nil
}
