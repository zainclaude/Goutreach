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
			// Real FastMoss shop fields: brand, seller_id, total_gmv, affiliate_creator_count.
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":2,"list":[
				{"seller_id":"S1","brand":"MaryRuth's","total_gmv":3947765.35,"affiliate_creator_count":11649},
				{"seller_id":"S2","brand":"MaryRuth's - Kids","total_gmv":1000,"affiliate_creator_count":3}
			]}}`))
		case pathShopVideo:
			if f, _ := req["filter"].(map[string]any); f["seller_id"] != "S1" {
				t.Errorf("videoList seller_id = %v", f["seller_id"])
			}
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
		_, _ = w.Write([]byte(`{"code":0,"data":{"total":1,"list":[{"seller_id":"X","brand":"Totally Different Co"}]}}`))
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
