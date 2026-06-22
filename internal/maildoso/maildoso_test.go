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
	c := New("pat_test", srv.URL)
	return c, srv
}

func TestVerifyKeyHitsMe(t *testing.T) {
	var path, auth string
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	defer srv.Close()
	if err := c.VerifyKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/user/me" {
		t.Errorf("path = %q, want /v1/user/me", path)
	}
	if auth != "Bearer pat_test" {
		t.Errorf("auth = %q", auth)
	}
}

func TestListMailboxesUnwrapsItems(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user/accounts-lookup" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"items":[
			{"id":7,"email_account":"a@x.com","password":"pw12345678AA","provider":"maildoso","status":"active","imap":{"imap_host":"imap.x.com","port":993}}
		],"meta":{}}`))
	})
	defer srv.Close()
	mbs, err := c.ListMailboxes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(mbs) != 1 || mbs[0].Email != "a@x.com" || mbs[0].IMAP == nil || mbs[0].IMAP.Host != "imap.x.com" || mbs[0].IMAP.Port != 993 {
		t.Fatalf("unexpected: %+v", mbs[0])
	}
}

func TestCreateMailboxesPostsInserts(t *testing.T) {
	var body []map[string]any
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user/accounts" {
			t.Errorf("path = %q", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(202)
	})
	defer srv.Close()
	if err := c.CreateMailboxes(context.Background(), []string{"jane@x.com"}, "maildoso"); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body[0]["email_account"] != "jane@x.com" || body[0]["provider"] != "maildoso" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestBaseURLDefaultAndTrim(t *testing.T) {
	if c := New("k", ""); c.baseURL != DefaultBaseURL {
		t.Errorf("empty base should default, got %q", c.baseURL)
	}
	if c := New("k", "https://x.com/"); c.baseURL != "https://x.com" {
		t.Errorf("trailing slash not trimmed: %q", c.baseURL)
	}
}
