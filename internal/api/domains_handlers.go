package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/zainclaude/goutreach/internal/porkbun"
	"github.com/zainclaude/goutreach/internal/store"
)

// Settings keys for the Porkbun registrar integration (both stored encrypted).
const (
	keyPorkbunAPIKey    = "porkbun_api_key"
	keyPorkbunSecretKey = "porkbun_secret_key"
)

// porkbunClient builds a Porkbun client from the user's encrypted settings.
func (s *Server) porkbunClient(ctx context.Context, userID int64) (*porkbun.Client, error) {
	encKey, ok, _ := s.st.GetSetting(ctx, userID, keyPorkbunAPIKey)
	if !ok || encKey == "" {
		return nil, fmt.Errorf("Porkbun API key not configured — add it under Settings")
	}
	encSecret, ok, _ := s.st.GetSetting(ctx, userID, keyPorkbunSecretKey)
	if !ok || encSecret == "" {
		return nil, fmt.Errorf("Porkbun secret key not configured — add it under Settings")
	}
	apiKey, err := s.cipher.Decrypt(encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt Porkbun API key: %w", err)
	}
	secret, err := s.cipher.Decrypt(encSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt Porkbun secret key: %w", err)
	}
	return porkbun.New(apiKey, secret), nil
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) {
	ds, err := s.st.ListDomains(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ds)
}

// handlePorkbunPing checks that the saved Porkbun credentials work.
func (s *Server) handlePorkbunPing(w http.ResponseWriter, r *http.Request) {
	pb, err := s.porkbunClient(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ip, err := pb.Ping(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ip": ip})
}

// handleCheckDomain returns availability + price for a candidate domain.
func (s *Server) handleCheckDomain(w http.ResponseWriter, r *http.Request) {
	domain := normalizeDomain(r.URL.Query().Get("domain"))
	if domain == "" {
		writeErr(w, http.StatusBadRequest, "domain query param required")
		return
	}
	pb, err := s.porkbunClient(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	avail, err := pb.CheckDomain(r.Context(), domain)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, avail)
}

type registerDomainReq struct {
	Domain       string `json:"domain"`
	Years        int    `json:"years"`
	WhoisPrivacy bool   `json:"whois_privacy"`
	AutoRenew    bool   `json:"auto_renew"`
}

// handleRegisterDomain purchases a domain through Porkbun (beta API) and records
// it. On failure (e.g. TLD not API-registerable) it returns the registrar error
// so the UI can suggest manual purchase + Import.
func (s *Server) handleRegisterDomain(w http.ResponseWriter, r *http.Request) {
	var req registerDomainReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	domain := normalizeDomain(req.Domain)
	if domain == "" {
		writeErr(w, http.StatusBadRequest, "domain required")
		return
	}
	userID := s.userID(r)
	pb, err := s.porkbunClient(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := pb.Register(r.Context(), domain, porkbun.RegisterOptions{
		Years:        req.Years,
		WhoisPrivacy: req.WhoisPrivacy,
		AutoRenew:    req.AutoRenew,
	}); err != nil {
		writeErr(w, http.StatusBadGateway, "registration failed: "+err.Error()+
			" — if this TLD isn't API-registerable, buy it on Porkbun then use Import")
		return
	}
	d, err := s.st.CreateDomain(r.Context(), store.Domain{
		UserID: userID, Domain: domain, Registrar: "porkbun", Status: "purchased",
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleImportDomain records a domain already owned at Porkbun so PipelineBuilder
// can manage its DNS and provision mailboxes on it.
func (s *Server) handleImportDomain(w http.ResponseWriter, r *http.Request) {
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
	d, err := s.st.CreateDomain(r.Context(), store.Domain{
		UserID: s.userID(r), Domain: domain, Registrar: "porkbun", Status: "purchased",
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleApplyDNS writes the cold-email DNS records (MX, SPF, DMARC, and DKIM when
// available) for a domain via Porkbun, upserting so it's safe to re-run.
func (s *Server) handleApplyDNS(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	d, err := s.st.GetDomain(r.Context(), userID, idParam(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, "domain not found")
		return
	}
	pb, err := s.porkbunClient(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	desired := coldEmailRecords(d.Domain, d.DKIMRecord)
	existing, err := pb.RetrieveDNS(r.Context(), d.Domain)
	if err != nil {
		_ = s.st.SetDomainStatus(r.Context(), userID, d.ID, "error", err.Error())
		writeErr(w, http.StatusBadGateway, "could not read existing DNS: "+err.Error())
		return
	}

	var applied []string
	for _, rec := range desired {
		fqdn := recordFQDN(d.Domain, rec.Name)
		if id := findRecordID(existing, rec.Type, fqdn, rec.Content); id != "" {
			err = pb.EditDNS(r.Context(), d.Domain, id, rec)
		} else {
			_, err = pb.CreateDNS(r.Context(), d.Domain, rec)
		}
		if err != nil {
			_ = s.st.SetDomainStatus(r.Context(), userID, d.ID, "error", err.Error())
			writeErr(w, http.StatusBadGateway, fmt.Sprintf("failed to apply %s record: %s", rec.Type, err.Error()))
			return
		}
		applied = append(applied, rec.Type)
	}

	_ = s.st.SetDomainDNSApplied(r.Context(), userID, d.ID, true)
	_ = s.st.SetDomainStatus(r.Context(), userID, d.ID, "dns_ready", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"applied":  applied,
		"dkim_set": d.DKIMRecord != "",
	})
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteDomain(r.Context(), s.userID(r), idParam(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// coldEmailRecords builds the standard DNS records for a Google-Workspace-backed
// cold-email domain. DKIM is only included once the provider has supplied a key.
func coldEmailRecords(domain, dkim string) []porkbun.DNSRecordInput {
	recs := []porkbun.DNSRecordInput{
		// Google Workspace's single modern MX record.
		{Name: "", Type: "MX", Content: "smtp.google.com", Prio: 1, TTL: 3600},
		// SPF authorizing Google to send for the domain.
		{Name: "", Type: "TXT", Content: "v=spf1 include:_spf.google.com ~all", TTL: 3600},
		// DMARC starting in monitor mode (safe during warmup).
		{Name: "_dmarc", Type: "TXT", Content: "v=DMARC1; p=none; sp=none; adkim=r; aspf=r; rua=mailto:postmaster@" + domain, TTL: 3600},
	}
	if dkim != "" {
		recs = append(recs, porkbun.DNSRecordInput{
			Name: "google._domainkey", Type: "TXT", Content: dkim, TTL: 3600,
		})
	}
	return recs
}

// recordFQDN returns the fully-qualified host for a subdomain ("" => root).
func recordFQDN(domain, sub string) string {
	if sub == "" {
		return domain
	}
	return sub + "." + domain
}

// findRecordID locates an existing record to update. For TXT records it matches
// on the value's leading token (e.g. "v=spf1", "v=DMARC1") so unrelated TXT
// records at the same host are left untouched; other types match on host+type.
func findRecordID(records []porkbun.DNSRecord, recType, fqdn, content string) string {
	for _, ex := range records {
		if !strings.EqualFold(ex.Type, recType) || !strings.EqualFold(ex.Name, fqdn) {
			continue
		}
		if recType == "TXT" {
			if txtKind(ex.Content) == txtKind(content) {
				return ex.ID
			}
			continue
		}
		return ex.ID
	}
	return ""
}

// txtKind classifies a TXT value so we update the matching one in place.
func txtKind(v string) string {
	v = strings.ToLower(strings.TrimSpace(strings.Trim(v, `"`)))
	switch {
	case strings.HasPrefix(v, "v=spf1"):
		return "spf"
	case strings.HasPrefix(v, "v=dmarc1"):
		return "dmarc"
	case strings.HasPrefix(v, "v=dkim1"), strings.Contains(v, "k=rsa"):
		return "dkim"
	}
	return v
}

// normalizeDomain lowercases and strips scheme, path, and a leading "www.".
func normalizeDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(s, "www.")
	return strings.TrimSuffix(s, ".")
}
