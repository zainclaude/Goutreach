package api

import (
	"strings"
	"testing"

	"github.com/zainclaude/goutreach/internal/porkbun"
)

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"Example.com":              "example.com",
		"https://www.example.com/": "example.com",
		"  http://Example.com/x ":  "example.com",
		"www.Brand.io":             "brand.io",
		"example.com.":             "example.com",
	}
	for in, want := range cases {
		if got := normalizeDomain(in); got != want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRecordFQDN(t *testing.T) {
	if got := recordFQDN("example.com", ""); got != "example.com" {
		t.Errorf("root fqdn = %q", got)
	}
	if got := recordFQDN("example.com", "_dmarc"); got != "_dmarc.example.com" {
		t.Errorf("dmarc fqdn = %q", got)
	}
}

func TestColdEmailRecords(t *testing.T) {
	without := coldEmailRecords("example.com", "")
	if len(without) != 3 {
		t.Fatalf("expected 3 records without DKIM, got %d", len(without))
	}
	with := coldEmailRecords("example.com", "v=DKIM1; k=rsa; p=ABC")
	if len(with) != 4 {
		t.Fatalf("expected 4 records with DKIM, got %d", len(with))
	}
	// DMARC rua should reference the domain.
	var sawDMARC bool
	for _, r := range without {
		if r.Name == "_dmarc" && r.Type == "TXT" {
			sawDMARC = true
			if want := "postmaster@example.com"; !strings.Contains(r.Content, want) {
				t.Errorf("DMARC missing %q: %s", want, r.Content)
			}
		}
	}
	if !sawDMARC {
		t.Error("no DMARC record generated")
	}
}

func TestFindRecordID_TXTMatchesByKind(t *testing.T) {
	existing := []porkbun.DNSRecord{
		{ID: "1", Name: "example.com", Type: "TXT", Content: "google-site-verification=abc"},
		{ID: "2", Name: "example.com", Type: "TXT", Content: "v=spf1 include:_spf.google.com ~all"},
		{ID: "3", Name: "example.com", Type: "MX", Content: "old-mx.example.com"},
	}
	// SPF should match the existing SPF record (id 2), not the verification TXT.
	if id := findRecordID(existing, "TXT", "example.com", "v=spf1 include:_spf.google.com ~all"); id != "2" {
		t.Errorf("SPF match = %q, want 2", id)
	}
	// A new DMARC TXT has no existing match.
	if id := findRecordID(existing, "TXT", "_dmarc.example.com", "v=DMARC1; p=none"); id != "" {
		t.Errorf("DMARC match = %q, want empty", id)
	}
	// MX matches by type+host.
	if id := findRecordID(existing, "MX", "example.com", "smtp.google.com"); id != "3" {
		t.Errorf("MX match = %q, want 3", id)
	}
}
