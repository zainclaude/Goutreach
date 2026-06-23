package api

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zainclaude/goutreach/internal/mailer"
	"github.com/zainclaude/goutreach/internal/store"
)

type accountReq struct {
	Provider           string `json:"provider"` // "", "gmail"/"google", "outlook"/"office365"
	Email              string `json:"email"`
	FromName           string `json:"from_name"`
	Password           string `json:"password"` // shared password (used for both SMTP+IMAP when set)
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
	SkipVerify         bool   `json:"skip_verify"` // save without dialing SMTP/IMAP
}

// providerPreset holds the SMTP/IMAP servers for a known provider.
type providerPreset struct{ smtpHost, imapHost string }

var providerPresets = map[string]providerPreset{
	"gmail":     {"smtp.gmail.com", "imap.gmail.com"},
	"google":    {"smtp.gmail.com", "imap.gmail.com"},
	"outlook":   {"smtp-mail.outlook.com", "outlook.office365.com"},
	"office365": {"smtp.office365.com", "outlook.office365.com"},
	"microsoft": {"smtp-mail.outlook.com", "outlook.office365.com"},
}

func (a *accountReq) applyDefaults() {
	// Provider presets fill in hosts so the user only needs email + password.
	if p, ok := providerPresets[strings.ToLower(a.Provider)]; ok {
		if a.SMTPHost == "" {
			a.SMTPHost = p.smtpHost
		}
		if a.IMAPHost == "" {
			a.IMAPHost = p.imapHost
		}
	}
	// A single shared password populates both SMTP and IMAP.
	if a.Password != "" {
		if a.SMTPPassword == "" {
			a.SMTPPassword = a.Password
		}
		if a.IMAPPassword == "" {
			a.IMAPPassword = a.Password
		}
	}
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
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.applyDefaults()
	if req.Email == "" || req.SMTPHost == "" || req.IMAPHost == "" {
		writeErr(w, http.StatusBadRequest, "email is required (and host/port for custom providers)")
		return
	}
	if !req.SkipVerify {
		if err := verifyCreds(req); err != nil {
			writeErr(w, http.StatusBadRequest, "verification failed: "+err.Error())
			return
		}
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
	if req.SkipVerify {
		_ = s.st.SetAccountStatus(r.Context(), acc.ID, "unverified", "added without verification")
		acc.Status, acc.LastError = "unverified", "added without verification"
	}
	writeJSON(w, http.StatusOK, acc)
}

func (s *Server) handleWarmupStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.st.WarmupStats(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
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

// handleImportAccounts bulk-connects mailboxes from a CSV. Columns (case-insensitive):
// email, password, provider, from_name, smtp_host, smtp_port, imap_host, imap_port,
// daily_limit, warmup, warmup_target. Each row is verified concurrently before saving.
func (s *Server) handleImportAccounts(w http.ResponseWriter, r *http.Request) {
	body := csvReader(r)
	if body == nil {
		writeErr(w, http.StatusBadRequest, "no CSV provided")
		return
	}
	reader := csv.NewReader(body)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil || len(rows) < 2 {
		writeErr(w, http.StatusBadRequest, "could not parse CSV (need a header row + at least one account)")
		return
	}

	idx := headerIndex(rows[0])

	// When skip_verify is set we don't dial SMTP/IMAP first. This lets accounts
	// import even when the mail host is briefly unreachable from the server
	// (the verification dial would otherwise time out and drop the row).
	skipVerify := isTrue(r.FormValue("skip_verify"))

	type rowErr struct {
		Email string `json:"email"`
		Error string `json:"error"`
	}
	var (
		mu         sync.Mutex
		added      int
		unverified int
		errs       []rowErr
		warnings   []rowErr
		wg         sync.WaitGroup
		sem        = make(chan struct{}, 6) // bound concurrent verifications
		userID     = s.userID(r)
	)

	for _, row := range rows[1:] {
		req := parseAccountRow(idx, row)
		if req.Email == "" {
			continue
		}
		req.applyDefaults()

		wg.Add(1)
		go func(req accountReq) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Verify if asked, but a failure no longer drops the row — we save
			// it as "unverified" so the user gets their account and can retry.
			verifyErr := ""
			if !skipVerify {
				if err := verifyCreds(req); err != nil {
					verifyErr = err.Error()
				}
			}

			smtpEnc, _ := s.cipher.Encrypt(req.SMTPPassword)
			imapEnc, _ := s.cipher.Encrypt(req.IMAPPassword)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			acc, err := s.st.CreateAccount(ctx, store.EmailAccount{
				UserID: userID, Email: trimLower(req.Email), FromName: req.FromName,
				SMTPHost: req.SMTPHost, SMTPPort: req.SMTPPort, SMTPUsername: req.SMTPUsername, SMTPPasswordEnc: smtpEnc,
				IMAPHost: req.IMAPHost, IMAPPort: req.IMAPPort, IMAPUsername: req.IMAPUsername, IMAPPasswordEnc: imapEnc,
				DailyLimit: req.DailyLimit, WarmupEnabled: req.WarmupEnabled, WarmupTargetPerDay: req.WarmupTargetPerDay,
				Source: "csv",
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, rowErr{req.Email, err.Error()})
				mu.Unlock()
				return
			}
			// Flag unverified accounts so they're visible in the list; CreateAccount
			// defaults new rows to "active".
			if verifyErr != "" {
				_ = s.st.SetAccountStatus(context.Background(), acc.ID, "unverified", "verify: "+verifyErr)
			}
			mu.Lock()
			if verifyErr != "" {
				unverified++
				warnings = append(warnings, rowErr{req.Email, verifyErr})
			} else {
				added++
			}
			mu.Unlock()
		}(req)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{
		"added": added, "unverified": unverified, "failed": len(errs),
		"errors": errs, "warnings": warnings,
	})
}

// isTrue parses common truthy form values.
func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// headerIndex maps normalized CSV header names ("IMAP Host" -> "imap_host") to
// their column index.
func headerIndex(header []string) map[string]int {
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.ReplaceAll(strings.TrimSpace(h), " ", "_"))] = i
	}
	return idx
}

// parseAccountRow maps one CSV row to an accountReq. It understands vendor exports
// (e.g. Maildoso/Primeforge) with separate IMAP/SMTP username+password, First/Last
// Name, and Warmup Enabled/Limit columns, as well as the simple shared-password
// format. Provider defaults to gmail when no provider and no SMTP host are given.
func parseAccountRow(idx map[string]int, row []string) accountReq {
	col := func(name string) string {
		if i, ok := idx[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	colAny := func(names ...string) string {
		for _, n := range names {
			if v := col(n); v != "" {
				return v
			}
		}
		return ""
	}
	fromName := col("from_name")
	if fromName == "" {
		fromName = strings.TrimSpace(col("first_name") + " " + col("last_name"))
	}
	req := accountReq{
		Provider:           col("provider"),
		Email:              col("email"),
		FromName:           fromName,
		Password:           col("password"),
		SMTPHost:           col("smtp_host"),
		SMTPPort:           atoiOr(col("smtp_port"), 0),
		SMTPUsername:       col("smtp_username"),
		SMTPPassword:       col("smtp_password"),
		IMAPHost:           col("imap_host"),
		IMAPPort:           atoiOr(col("imap_port"), 0),
		IMAPUsername:       col("imap_username"),
		IMAPPassword:       col("imap_password"),
		DailyLimit:         atoiOr(col("daily_limit"), 0),
		WarmupEnabled:      parseBool(colAny("warmup_enabled", "warmup")),
		WarmupTargetPerDay: atoiOr(colAny("warmup_target", "warmup_limit"), 0),
	}
	if req.Provider == "" && req.SMTPHost == "" {
		req.Provider = "gmail"
	}
	return req
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "y", "on":
		return true
	}
	return false
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteAccount(r.Context(), s.userID(r), idParam(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
