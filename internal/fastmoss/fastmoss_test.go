package fastmoss

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrandMetricsFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sek_test" {
			t.Errorf("auth = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		switch r.URL.Path {
		case pathShopSearch:
			// returns two shops; the exact-name one ("MaryRuth's") must win.
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":2,"list":[
				{"shop_id":"S1","shop_name":"MaryRuth's","usd_gmv":3947765.35},
				{"shop_id":"S2","shop_name":"MaryRuth's - Kids","usd_gmv":1000}
			]}}`))
		case pathShopCreator:
			if f, _ := req["filter"].(map[string]any); f["shop_id"] != "S1" {
				t.Errorf("creatorList shop_id = %v", f["shop_id"])
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":11649,"list":[]}}`))
		case pathShopVideo:
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":17120,"list":[]}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("sek_test", srv.URL)
	m, err := c.BrandMetrics(context.Background(), "MaryRuth's")
	if err != nil {
		t.Fatal(err)
	}
	if !m.Found || m.ShopName != "MaryRuth's" {
		t.Fatalf("unexpected shop: %+v", m)
	}
	if m.RevenueUSD != 3947765.35 || m.Creators != 11649 || m.Videos != 17120 {
		t.Fatalf("metrics = %+v", m)
	}
}

func TestBrandMetricsNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"total":1,"list":[{"shop_id":"X","shop_name":"Totally Different Co"}]}}`))
	}))
	defer srv.Close()
	m, err := New("k", srv.URL).BrandMetrics(context.Background(), "MaryRuth's")
	if err != nil {
		t.Fatal(err)
	}
	if m.Found {
		t.Fatalf("should not match an unrelated shop: %+v", m)
	}
}

func TestEnvelopeErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":30003,"msg":"Access frequency limit exceeded"}`))
	}))
	defer srv.Close()
	_, err := New("k", srv.URL).BrandMetrics(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "Access frequency") {
		t.Fatalf("expected surfaced API error, got %v", err)
	}
}
