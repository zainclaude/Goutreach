package porkbun

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a client at an httptest server.
func newTestClient(h http.HandlerFunc) (*Client, *httptest.Server) {
	srv := httptest.NewServer(h)
	c := New("pk_test", "sk_test")
	c.baseURL = srv.URL
	return c, srv
}

func TestCheckDomainAvailable(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/domain/checkDomain/example.com") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["apikey"] != "pk_test" || got["secretapikey"] != "sk_test" {
			t.Errorf("credentials not in body: %v", got)
		}
		_, _ = w.Write([]byte(`{"status":"SUCCESS","response":{"avail":"yes","price":"9.73","premium":"no"}}`))
	})
	defer srv.Close()

	a, err := c.CheckDomain(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Available || a.Price != "9.73" || a.Premium {
		t.Fatalf("unexpected availability: %+v", a)
	}
}

func TestErrorStatusSurfacesMessage(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"Invalid API key"}`))
	})
	defer srv.Close()

	_, err := c.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("expected surfaced error, got %v", err)
	}
}

func TestCreateDNSSendsRecordFields(t *testing.T) {
	var got map[string]any
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"status":"SUCCESS","id":1234567}`))
	})
	defer srv.Close()

	id, err := c.CreateDNS(context.Background(), "example.com", DNSRecordInput{
		Name: "_dmarc", Type: "TXT", Content: "v=DMARC1; p=none", TTL: 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "1234567" {
		t.Fatalf("expected id 1234567, got %q", id)
	}
	if got["name"] != "_dmarc" || got["type"] != "TXT" || got["ttl"] != "3600" {
		t.Fatalf("unexpected record body: %v", got)
	}
	if _, hasPrio := got["prio"]; hasPrio {
		t.Errorf("non-MX record should not send prio: %v", got)
	}
}

func TestCreateDNSMXIncludesPrio(t *testing.T) {
	var got map[string]any
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"status":"SUCCESS","id":1}`))
	})
	defer srv.Close()

	if _, err := c.CreateDNS(context.Background(), "example.com", DNSRecordInput{
		Type: "MX", Content: "smtp.google.com", Prio: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if got["prio"] != "1" {
		t.Fatalf("MX record should send prio=1, got %v", got)
	}
}
