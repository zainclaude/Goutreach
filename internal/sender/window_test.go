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
