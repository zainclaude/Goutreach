package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zainclaude/goutreach/internal/maildoso"
	"github.com/zainclaude/goutreach/internal/store"
)

// Settings keys for the Maildoso provisioning integration.
const (
	keyMaildosoAPIKey  = "maildoso_api_key"  // secret
	keyMaildosoBaseURL = "maildoso_base_url" // optional override (non-secret)
)

// maildosoClient builds a Maildoso client from the user's encrypted settings.
func (s *Server) maildosoClient(ctx context.Context, userID int64) (*maildoso.Client, error) {
	enc, ok, _ := s.st.GetSetting(ctx, userID, keyMaildosoAPIKey)
	if !ok || enc == "" {
		return nil, fmt.Errorf("Maildoso API key not configured — add it under Settings")
	}
	apiKey, err := s.cipher.Decrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("decrypt Maildoso API key: %w", err)
	}
	baseURL, _, _ := s.st.GetSetting(ctx, userID, keyMaildosoBaseURL)
	return maildoso.New(apiKey, baseURL), nil
}

// handleMaildosoPing verifies the saved Maildoso credentials.
func (s *Server) handleMaildosoPing(w http.ResponseWriter, r *http.Request) {
	mc, err := s.maildosoClient(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := mc.VerifyKey(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMaildosoListDomains(w http.ResponseWriter, r *http.Request) {
	mc, err := s.maildosoClient(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ds, err := mc.ListDomains(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

func (s *Server) handleMaildosoCreateDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	domain := normalizeDomain(req.Domain)
	if domain == "" {
		writeErr(w, http.StatusBadRequest, "domain required")
		return
	}
	mc, err := s.maildosoClient(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := mc.CreateDomain(r.Context(), domain); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// Mirror it into our domains table so it shows alongside Porkbun domains.
	_, _ = s.st.CreateDomain(r.Context(), store.Domain{
		UserID: s.userID(r), Domain: domain, Registrar: "maildoso", Status: "purchased",
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMaildosoOrderMailboxes provisions mailboxes on a Maildoso domain. Local
// parts are the part before the @ (e.g. "jane.doe"); the domain is the full
// domain name. Provisioning is async, so credentials appear on a later sync.
func (s *Server) handleMaildosoOrderMailboxes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain     string   `json:"domain"`
		LocalParts []string `json:"local_parts"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	domain := normalizeDomain(req.Domain)
	if domain == "" || len(req.LocalParts) == 0 {
		writeErr(w, http.StatusBadRequest, "domain and at least one inbox name required")
		return
	}
	emails := make([]string, 0, len(req.LocalParts))
	for _, lp := range req.LocalParts {
		lp = strings.TrimSpace(strings.TrimSuffix(lp, "@"+domain))
		if lp != "" {
			emails = append(emails, lp+"@"+domain)
		}
	}
	mc, err := s.maildosoClient(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := mc.CreateMailboxes(r.Context(), emails, "maildoso"); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ordered": len(emails)})
}

// handleMaildosoSync pulls every Maildoso mailbox that has credentials and
// connects it into email_accounts (tagged source=maildoso, deduped by email).
// Ready inboxes are verified concurrently before saving; ones still provisioning
// (no credentials yet) are reported as pending.
func (s *Server) handleMaildosoSync(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	mc, err := s.maildosoClient(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	mbs, err := mc.ListMailboxes(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	type rowErr struct {
		Email string `json:"email"`
		Error string `json:"error"`
	}
	var (
		mu      sync.Mutex
		added   int
		pending int
		errs    []rowErr
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 6)
	)

	for _, mb := range mbs {
		if mb.Email == "" || mb.Password == "" {
			pending++ // not provisioned yet — no credentials to connect
			continue
		}
		req := accountReq{Email: mb.Email, Password: mb.Password}
		if strings.EqualFold(mb.Provider, "google") {
			// Google Workspace mailbox: gmail presets fill SMTP/IMAP hosts.
			req.Provider = "gmail"
		} else {
			// Maildoso-hosted: it returns the IMAP host; derive SMTP from it.
			if mb.IMAP != nil {
				req.IMAPHost = mb.IMAP.Host
				req.IMAPPort = mb.IMAP.Port
				req.SMTPHost = strings.Replace(mb.IMAP.Host, "imap", "smtp", 1)
			}
		}
		req.applyDefaults()
		if req.SMTPHost == "" || req.IMAPHost == "" {
			pending++ // host info not available yet
			continue
		}

		wg.Add(1)
		go func(req accountReq, extID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := verifyCreds(req); err != nil {
				mu.Lock()
				errs = append(errs, rowErr{req.Email, err.Error()})
				mu.Unlock()
				return
			}
			smtpEnc, _ := s.cipher.Encrypt(req.SMTPPassword)
			imapEnc, _ := s.cipher.Encrypt(req.IMAPPassword)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, err := s.st.CreateAccount(ctx, store.EmailAccount{
				UserID: userID, Email: trimLower(req.Email),
				SMTPHost: req.SMTPHost, SMTPPort: req.SMTPPort, SMTPUsername: req.SMTPUsername, SMTPPasswordEnc: smtpEnc,
				IMAPHost: req.IMAPHost, IMAPPort: req.IMAPPort, IMAPUsername: req.IMAPUsername, IMAPPasswordEnc: imapEnc,
				DailyLimit: req.DailyLimit, WarmupTargetPerDay: req.WarmupTargetPerDay,
				Source: "maildoso", ExternalID: extID,
			})
			mu.Lock()
			if err != nil {
				errs = append(errs, rowErr{req.Email, err.Error()})
			} else {
				added++
			}
			mu.Unlock()
		}(req, fmt.Sprintf("%d", mb.ID))
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{
		"added": added, "pending": pending, "failed": len(errs), "errors": errs,
	})
}
