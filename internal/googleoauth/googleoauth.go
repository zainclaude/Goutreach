// Package googleoauth handles the "Sign in with Google" flow for connecting
// Gmail/Workspace mailboxes via OAuth (XOAUTH2) instead of app passwords.
package googleoauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Client wraps an OAuth2 config for Gmail access.
type Client struct {
	cfg     *oauth2.Config
	enabled bool
}

// New builds a Client. If client id/secret are empty the flow is disabled.
func New(clientID, clientSecret, redirectURL string) *Client {
	if clientID == "" || clientSecret == "" {
		return &Client{}
	}
	return &Client{
		enabled: true,
		cfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     google.Endpoint,
			// https://mail.google.com/ grants SMTP+IMAP via XOAUTH2.
			Scopes: []string{
				"https://mail.google.com/",
				"openid",
				"https://www.googleapis.com/auth/userinfo.email",
			},
		},
	}
}

// Enabled reports whether OAuth is configured.
func (c *Client) Enabled() bool { return c.enabled }

var errDisabled = errors.New("google oauth not configured")

// AuthCodeURL returns the consent URL. access_type=offline + prompt=consent
// ensures Google returns a refresh token.
func (c *Client) AuthCodeURL(state string) (string, error) {
	if !c.enabled {
		return "", errDisabled
	}
	return c.cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	), nil
}

// Exchange swaps an authorization code for tokens (including a refresh token).
func (c *Client) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	if !c.enabled {
		return nil, errDisabled
	}
	return c.cfg.Exchange(ctx, code)
}

// Email fetches the authorized account's email address.
func (c *Client) Email(ctx context.Context, tok *oauth2.Token) (string, error) {
	if !c.enabled {
		return "", errDisabled
	}
	httpClient := c.cfg.Client(ctx, tok)
	resp, err := httpClient.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo: status %d", resp.StatusCode)
	}
	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.Email == "" {
		return "", errors.New("no email in userinfo response")
	}
	return info.Email, nil
}

// AccessToken mints a fresh access token from a stored refresh token.
func (c *Client) AccessToken(ctx context.Context, refreshToken string) (string, error) {
	if !c.enabled {
		return "", errDisabled
	}
	if refreshToken == "" {
		return "", errors.New("empty refresh token")
	}
	ts := c.cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	tok, err := ts.Token()
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
