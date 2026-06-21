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
