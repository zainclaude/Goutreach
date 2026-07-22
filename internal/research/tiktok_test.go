package research

import (
	"testing"
	"time"
)

func TestParseKM(t *testing.T) {
	cases := map[string]int{
		"17.12k": 17120,
		"$3.95m": 3950000,
		"6.28k":  6280,
		"570":    570,
		"1,234":  1234,
		"2.5b":   2500000000,
		"":       0,
		"n/a":    0,
		"$0":     0,
	}
	for in, want := range cases {
		if got := parseKM(in); got != want {
			t.Errorf("parseKM(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPickSellerID(t *testing.T) {
	// Mirrors the real /overview/fullText/search response for "MaryRuth's":
	// exact normalized match must win the real shop over the $0 duplicate.
	mary := []sellerHit{
		{SellerID: "real", SellerName: "MaryRuth's", GMVin30: 3947765, Score: 82},
		{SellerID: "kids", SellerName: "MaryRuth's - Kids", GMVin30: 174174, Score: 54},
		{SellerID: "dup", SellerName: "Maryruths", GMVin30: 0, Score: 17},
	}
	if got := pickSellerID("MaryRuth's", mary); got != "real" {
		t.Errorf("MaryRuth's -> %q, want real", got)
	}
	// Containment fallback: "bissell" -> "BISSELL Clean".
	bissell := []sellerHit{{SellerID: "b1", SellerName: "BISSELL Clean", Score: 70}}
	if got := pickSellerID("bissell", bissell); got != "b1" {
		t.Errorf("bissell -> %q, want b1", got)
	}
	// No confident match -> empty (don't grab the wrong shop).
	if got := pickSellerID("Totally Unrelated Co", bissell); got != "" {
		t.Errorf("unrelated -> %q, want empty", got)
	}
	// A shop that's only a fragment of the brand name is a different company —
	// "Broken Arrow" (apparel) must not match "Broken Arrow Electric Supply".
	frag := []sellerHit{{SellerID: "f1", SellerName: "Broken Arrow", GMVin30: 271000, Score: 60}}
	if got := pickSellerID("Broken Arrow Electric Supply, Inc.", frag); got != "" {
		t.Errorf("fragment shop -> %q, want empty", got)
	}
	if got := pickSellerID("anything", nil); got != "" {
		t.Errorf("no hits -> %q, want empty", got)
	}
}

func TestLast30(t *testing.T) {
	start, end := last30()
	const f = "2006-01-02"
	st, err := time.Parse(f, start)
	if err != nil {
		t.Fatalf("bad start %q: %v", start, err)
	}
	en, err := time.Parse(f, end)
	if err != nil {
		t.Fatalf("bad end %q: %v", end, err)
	}
	if d := en.Sub(st).Hours() / 24; d != 29 {
		t.Errorf("window = %v days, want 29 (30 inclusive)", d)
	}
	// end should be in the past (yesterday-ish), not the future.
	if !en.Before(time.Now().UTC()) {
		t.Errorf("end %q is not before now", end)
	}
}
