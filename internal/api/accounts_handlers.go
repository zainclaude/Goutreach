package api

import (
	"context"
	"net/http"
	"time"

	"github.com/zainclaude/goutreach/internal/mailer"
	"github.com/zainclaude/goutreach/internal/store"
)

type accountReq struct {
	Email              string `json:"email"`
	FromName           string `json:"from_name"`
	SMTPHost           string `json:"smtp_host"`
	SMTPPort           int    `json:"smtp_port"`
	SMTPUsername       string `json:"smtp_username"`
	SMTPPassword       string `json:"smtp_password"`
	IMAPHost           string `json:"imap_host"`
	IMAPPort           int    `json:"imap_port"`
	IMAPUsername       string `json:"imap_username"`
	IMAPPassword       string `json:"imap_password"`
	DailyLimit         int    `json:"daily_limit"`
	WarmupEnabled      bool   `json:"warmup_enabled"`
	WarmupTargetPerDay int    `json:"warmup_target_per_day"`
}

func (a *accountReq) applyDefaults() {
	if a.SMTPPort == 0 {
		a.SMTPPort = 587
	}
	if a.IMAPPort == 0 {
		a.IMAPPort = 993
	}
	if a.SMTPUsername == "" {
		a.SMTPUsername = a.Email
	}
	if a.IMAPUsername == "" {
		a.IMAPUsername = a.Email
	}
	if a.DailyLimit == 0 {
		a.DailyLimit = 30
	}
	if a.WarmupTargetPerDay == 0 {
		a.WarmupTargetPerDay = 20
	}
}

// verifyCreds dials SMTP and IMAP to validate credentials.
func verifyCreds(req accountReq) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := mailer.VerifySMTP(ctx, mailer.SMTPCreds{
		Host: req.SMTPHost, Port: req.SMTPPort, Username: req.SMTPUsername, Password: req.SMTPPassword,
	}); err != nil {
		return err
	}
	return mailer.VerifyIMAP(ctx, mailer.IMAPCreds{
		Host: req.IMAPHost, Port: req.IMAPPort, Username: req.IMAPUsername, Password: req.IMAPPassword,
	})
}

func (s *Server) handleVerifyAccount(w http.ResponseWriter, r *http.Request) {
	var req accountReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.applyDefaults()
	if err := verifyCreds(req); err != nil {
		writeErr(w, http.StatusBadRequest, "verification failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req accountReq
	if err := readJSON(r, &req); err != nil || req.Email == "" || req.SMTPHost == "" || req.IMAPHost == "" {
		writeErr(w, http.StatusBadRequest, "email, smtp_host and imap_host are required")
		return
	}
	req.applyDefaults()
	if err := verifyCreds(req); err != nil {
		writeErr(w, http.StatusBadRequest, "verification failed: "+err.Error())
		return
	}
	smtpEnc, err := s.cipher.Encrypt(req.SMTPPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	imapEnc, err := s.cipher.Encrypt(req.IMAPPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	acc, err := s.st.CreateAccount(r.Context(), store.EmailAccount{
		UserID:             s.userID(r),
		Email:              trimLower(req.Email),
		FromName:           req.FromName,
		SMTPHost:           req.SMTPHost,
		SMTPPort:           req.SMTPPort,
		SMTPUsername:       req.SMTPUsername,
		SMTPPasswordEnc:    smtpEnc,
		IMAPHost:           req.IMAPHost,
		IMAPPort:           req.IMAPPort,
		IMAPUsername:       req.IMAPUsername,
		IMAPPasswordEnc:    imapEnc,
		DailyLimit:         req.DailyLimit,
		WarmupEnabled:      req.WarmupEnabled,
		WarmupTargetPerDay: req.WarmupTargetPerDay,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	accs, err := s.st.ListAccounts(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accs)
}

type updateAccountReq struct {
	FromName           string `json:"from_name"`
	DailyLimit         int    `json:"daily_limit"`
	WarmupEnabled      bool   `json:"warmup_enabled"`
	WarmupTargetPerDay int    `json:"warmup_target_per_day"`
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req updateAccountReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.DailyLimit < 0 {
		req.DailyLimit = 0
	}
	if err := s.st.UpdateAccountSettings(r.Context(), s.userID(r), idParam(r),
		req.FromName, req.DailyLimit, req.WarmupEnabled, req.WarmupTargetPerDay); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteAccount(r.Context(), s.userID(r), idParam(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
