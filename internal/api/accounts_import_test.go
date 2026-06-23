package api

import (
	"strings"
	"testing"
)

// The header used by Primeforge / Maildoso mailbox exports.
const vendorHeader = "Email,First Name,Last Name,IMAP Username,IMAP Password,IMAP Host,IMAP Port,SMTP Username,SMTP Password,SMTP Host,SMTP Port,Daily Limit,Warmup Enabled,Warmup Limit,Warmup Increment"

func TestParseAccountRowVendorExport(t *testing.T) {
	idx := headerIndex(strings.Split(vendorHeader, ","))
	row := strings.Split("jane@brand.com,Jane,Doe,jane@brand.com,imapPW123,imap.gmail.com,993,jane@brand.com,smtpPW123,smtp.gmail.com,587,40,TRUE,45,3", ",")

	req := parseAccountRow(idx, row)
	req.applyDefaults()

	checks := map[string]string{
		"email":     req.Email,
		"from_name": req.FromName,
		"smtp_host": req.SMTPHost,
		"smtp_user": req.SMTPUsername,
		"smtp_pass": req.SMTPPassword,
		"imap_host": req.IMAPHost,
		"imap_user": req.IMAPUsername,
		"imap_pass": req.IMAPPassword,
	}
	want := map[string]string{
		"email": "jane@brand.com", "from_name": "Jane Doe",
		"smtp_host": "smtp.gmail.com", "smtp_user": "jane@brand.com", "smtp_pass": "smtpPW123",
		"imap_host": "imap.gmail.com", "imap_user": "jane@brand.com", "imap_pass": "imapPW123",
	}
	for k, w := range want {
		if checks[k] != w {
			t.Errorf("%s = %q, want %q", k, checks[k], w)
		}
	}
	if req.SMTPPort != 587 || req.IMAPPort != 993 {
		t.Errorf("ports = %d/%d, want 587/993", req.SMTPPort, req.IMAPPort)
	}
	if req.DailyLimit != 40 {
		t.Errorf("daily_limit = %d, want 40", req.DailyLimit)
	}
	if !req.WarmupEnabled {
		t.Error("warmup should be enabled (TRUE)")
	}
	if req.WarmupTargetPerDay != 45 {
		t.Errorf("warmup target = %d, want 45 (from Warmup Limit)", req.WarmupTargetPerDay)
	}
}

func TestParseAccountRowSimpleDefaultsGmail(t *testing.T) {
	// Minimal CSV: just email + password, no hosts -> defaults to gmail presets.
	idx := headerIndex([]string{"email", "password"})
	req := parseAccountRow(idx, []string{"a@x.com", "apppw"})
	req.applyDefaults()
	if req.Provider != "gmail" || req.SMTPHost != "smtp.gmail.com" || req.IMAPHost != "imap.gmail.com" {
		t.Fatalf("expected gmail presets, got provider=%q smtp=%q imap=%q", req.Provider, req.SMTPHost, req.IMAPHost)
	}
	if req.SMTPPassword != "apppw" || req.IMAPPassword != "apppw" {
		t.Errorf("shared password not applied: smtp=%q imap=%q", req.SMTPPassword, req.IMAPPassword)
	}
}
