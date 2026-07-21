package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/zainclaude/goutreach/internal/store"
)

// handleGoogleStart returns the Google consent URL. The frontend redirects the
// browser to it. State carries a short-lived signed token identifying the user.
func (s *Server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	if !s.google.Enabled() {
		writeErr(w, http.StatusBadRequest, "Google OAuth is not configured (set GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET)")
		return
	}
	// Reuse the auth signer to mint a stateless, signed state token.
	state, err := s.auth.IssueToken(s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	url, err := s.google.AuthCodeURL(state)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// handleGoogleCallback handles Google's redirect: exchange the code, fetch the
// account email, store an OAuth-backed email account, then bounce to the UI.
func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if !s.google.Enabled() {
		http.Error(w, "Google OAuth not configured", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Error(w, "missing code/state", http.StatusBadRequest)
		return
	}
	userID, err := s.auth.ParseToken(state)
	if err != nil {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tok, err := s.google.Exchange(ctx, code)
	if err != nil {
		s.redirectErr(w, r, "token exchange failed")
		return
	}
	if tok.RefreshToken == "" {
		s.redirectErr(w, r, "Google did not return a refresh token; remove the app's access at myaccount.google.com/permissions and reconnect")
		return
	}
	email, err := s.google.Email(ctx, tok)
	if err != nil {
		s.redirectErr(w, r, "could not read account email")
		return
	}
	refreshEnc, err := s.cipher.Encrypt(tok.RefreshToken)
	if err != nil {
		s.redirectErr(w, r, "encrypt failed")
		return
	}

	_, err = s.st.CreateAccount(ctx, store.EmailAccount{
		UserID:               userID,
		Email:                email,
		FromName:             "",
		SMTPHost:             "smtp.gmail.com",
		SMTPPort:             587,
		SMTPUsername:         email,
		IMAPHost:             "imap.gmail.com",
		IMAPPort:             993,
		IMAPUsername:         email,
		DailyLimit:           30,
		WarmupEnabled:        true,
		WarmupTargetPerDay:   20,
		AuthType:             "oauth",
		OAuthRefreshTokenEnc: refreshEnc,
	})
	if err != nil {
		s.redirectErr(w, r, "could not save account")
		return
	}
	http.Redirect(w, r, "/accounts?connected=google", http.StatusFound)
}

func (s *Server) redirectErr(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/accounts?error="+url.QueryEscape(msg), http.StatusFound)
}
