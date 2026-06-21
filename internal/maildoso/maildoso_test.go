package maildoso

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(h http.HandlerFunc) (*Client, *httptest.Server) {
	srv := httptest.NewServer(h)
	c := New("md_test", srv.URL)
	return c, srv
}

func TestAuthHeaderAndListMailboxes(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer md_test" {
			t.Errorf("auth header = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"m1","email":"a@x.com","password":"p","smtp_host":"smtp.x.com","smtp_port":587,"imap_host":"imap.x.com","imap_port":993,"status":"active"}
		]}`))
	})
	defer srv.Close()

	mbs, err := c.ListMailboxes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mbs) != 1 || mbs[0].Email != "a@x.com" || mbs[0].SMTPPort != 587 {
		t.Fatalf("unexpected mailboxes: %+v", mbs)
	}
}

func TestCreateMailboxesSendsSpecs(t *testing.T) {
	var body map[string]any
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"data":[{"id":"m1","email":"jane@x.com"}]}`))
	})
	defer srv.Close()

	_, err := c.CreateMailboxes(context.Background(), "dom1", []MailboxSpec{{LocalPart: "jane"}})
	if err != nil {
		t.Fatal(err)
	}
	if body["domain_id"] != "dom1" {
		t.Fatalf("expected domain_id dom1, got %v", body["domain_id"])
	}
	if _, ok := body["mailboxes"]; !ok {
		t.Fatalf("mailboxes missing from body: %v", body)
	}
}

func TestErrorStatusSurfaced(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	})
	defer srv.Close()

	if err := c.VerifyKey(context.Background()); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestBaseURLDefaultAndTrim(t *testing.T) {
	if c := New("k", ""); c.baseURL != DefaultBaseURL {
		t.Errorf("empty base should default, got %q", c.baseURL)
	}
	if c := New("k", "https://x.com/v1/"); c.baseURL != "https://x.com/v1" {
		t.Errorf("trailing slash not trimmed: %q", c.baseURL)
	}
}
