// Package mailauth resolves a stored email account into ready-to-use SMTP/IMAP
// credentials, handling both password and OAuth (XOAUTH2) auth types.
package mailauth

import (
	"context"
	"errors"

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
	c := mailer.IMAPCreds{Host: a.IMAPHost, Port: a.IMAPPort, Username: a.IMAPUsername}
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
