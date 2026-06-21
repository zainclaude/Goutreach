// Package mailer wraps SMTP sending and IMAP polling for connected mailboxes.
package mailer

import (
	"context"
	"fmt"
	"time"

	"github.com/wneessen/go-mail"
)

// SMTPCreds describes how to authenticate to a sending server.
type SMTPCreds struct {
	Host     string
	Port     int
	Username string
	Password string
}

// OutgoingEmail is a message to send.
type OutgoingEmail struct {
	FromAddr  string
	FromName  string
	ToAddr    string
	ToName    string
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
		mail.WithPassword(c.Password),
		mail.WithSMTPAuth(mail.SMTPAuthPlain),
		mail.WithTimeout(30 * time.Second),
	}
	if c.Port == 465 {
		opts = append(opts, mail.WithSSL())
	} else {
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	}
	return mail.NewClient(c.Host, opts...)
}

// VerifySMTP dials and authenticates without sending, to validate credentials.
func VerifySMTP(ctx context.Context, c SMTPCreds) error {
	client, err := newClient(c)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	if err := client.DialWithContext(ctx); err != nil {
		return fmt.Errorf("smtp dial/auth: %w", err)
	}
	return client.Close()
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
