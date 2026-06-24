// Package mailauth resolves a stored email account into ready-to-use SMTP/IMAP
// credentials, handling both password and OAuth (XOAUTH2) auth types.
package mailauth

import (
	"context"
	"errors"
	"strings"

	"github.com/zainclaude/goutreach/internal/crypto"
	"github.com/zainclaude/goutreach/internal/googleoauth"
	"github.com/zainclaude/goutreach/internal/mailer"
	"github.com/zainclaude/goutreach/internal/store"
)

// Resolver builds live SMTP/IMAP credentials for an account.
type Resolver struct {
	cipher *crypto.Cipher
	google *googleoauth.Client
}

// New builds a Resolver. google may be nil/disabled if OAuth isn't configured.
func New(cipher *crypto.Cipher, google *googleoauth.Client) *Resolver {
	return &Resolver{cipher: cipher, google: google}
}

// SMTP returns sending credentials for an account.
func (r *Resolver) SMTP(ctx context.Context, a store.EmailAccount) (mailer.SMTPCreds, error) {
	c := mailer.SMTPCreds{Host: a.SMTPHost, Port: a.SMTPPort, Username: a.SMTPUsername}
	if a.AuthType == "oauth" {
		tok, err := r.accessToken(ctx, a)
		if err != nil {
			return c, err
		}
		c.Username = a.Email
		c.OAuthToken = tok
		return c, nil
	}
	pw, err := r.cipher.Decrypt(a.SMTPPasswordEnc)
	if err != nil {
		return c, err
	}
	c.Password = pw
	return c, nil
}

// IMAP returns receiving credentials for an account.
func (r *Resolver) IMAP(ctx context.Context, a store.EmailAccount) (mailer.IMAPCreds, error) {
	host, port := imapEndpoint(a)
	c := mailer.IMAPCreds{Host: host, Port: port, Username: a.IMAPUsername}
	if a.AuthType == "oauth" {
		tok, err := r.accessToken(ctx, a)
		if err != nil {
			return c, err
		}
		c.Username = a.Email
		c.OAuthToken = tok
		return c, nil
	}
	pw, err := r.cipher.Decrypt(a.IMAPPasswordEnc)
	if err != nil {
		return c, err
	}
	c.Password = pw
	return c, nil
}

func (r *Resolver) accessToken(ctx context.Context, a store.EmailAccount) (string, error) {
	if r.google == nil || !r.google.Enabled() {
		return "", errors.New("oauth account but google oauth is not configured on the server")
	}
	rt, err := r.cipher.Decrypt(a.OAuthRefreshTokenEnc)
	if err != nil {
		return "", err
	}
	return r.google.AccessToken(ctx, rt)
}

// imapEndpoint returns the IMAP host/port to actually connect to. For a known
// provider (inferred from the SMTP host) it returns that provider's IMAP server,
// overriding any mis-stored imap_host — a Google mailbox always reads from
// imap.gmail.com even when the account row holds a custom/domain host, which is a
// common misconfiguration that makes the connection drop with "unexpected EOF".
func imapEndpoint(a store.EmailAccount) (string, int) {
	host, port := a.IMAPHost, a.IMAPPort
	if h := strings.ToLower(a.SMTPHost); strings.Contains(h, "gmail.com") || strings.Contains(h, "googlemail.com") {
		host, port = "imap.gmail.com", 993
	} else if strings.Contains(h, "outlook.com") || strings.Contains(h, "office365.com") {
		host, port = "outlook.office365.com", 993
	}
	if port == 0 {
		port = 993
	}
	return host, port
}
