package sender

import (
	"testing"
	"time"

	"github.com/zainclaude/goutreach/internal/store"
)

func TestInSendWindow(t *testing.T) {
	c := store.Campaign{
		Timezone:      "UTC",
		SendStartHour: 9,
		SendEndHour:   17,
		SendWeekdays:  []int32{1, 2, 3, 4, 5}, // Mon–Fri
	}
	// Wednesday 2024-01-03 is a weekday.
	wedNoon := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
	if !inSendWindow(c, wedNoon) {
		t.Error("expected Wed noon to be in window")
	}
	wedEarly := time.Date(2024, 1, 3, 7, 0, 0, 0, time.UTC)
	if inSendWindow(c, wedEarly) {
		t.Error("expected Wed 7am to be outside window")
	}
	// Saturday 2024-01-06.
	satNoon := time.Date(2024, 1, 6, 12, 0, 0, 0, time.UTC)
	if inSendWindow(c, satNoon) {
		t.Error("expected Saturday to be outside window")
	}
}

func TestNextWindowOpen(t *testing.T) {
	c := store.Campaign{Timezone: "UTC", SendStartHour: 9, SendEndHour: 17, SendWeekdays: []int32{1, 2, 3, 4, 5}}
	// Friday 6pm -> next open should be Monday 9am.
	friEve := time.Date(2024, 1, 5, 18, 0, 0, 0, time.UTC)
	next := nextWindowOpen(c, friEve)
	if next.Weekday() != time.Monday || next.Hour() != 9 {
		t.Errorf("expected Monday 9am, got %v", next)
	}
}

func TestSendWindowHonorsEastern(t *testing.T) {
	c := store.Campaign{Timezone: "America/New_York", SendStartHour: 9, SendEndHour: 17, SendWeekdays: []int32{1, 2, 3, 4, 5}}
	// Wed 2026-06-24, 18:00 UTC = 14:00 EDT -> inside the EST window.
	// (If tzdata didn't load and it fell back to UTC, 18:00 would be OUT, failing this.)
	if !inSendWindow(c, time.Date(2026, 6, 24, 18, 0, 0, 0, time.UTC)) {
		t.Error("18:00 UTC (14:00 EDT, Wed) should be inside the Eastern window")
	}
	// Wed 2026-06-24, 02:00 UTC = 22:00 EDT Tue -> outside.
	if inSendWindow(c, time.Date(2026, 6, 24, 2, 0, 0, 0, time.UTC)) {
		t.Error("02:00 UTC (22:00 EDT) should be outside the window")
	}
}
