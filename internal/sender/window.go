package sender

import (
	"time"
	_ "time/tzdata" // embed the tz database so LoadLocation works regardless of base image

	"github.com/zainclaude/goutreach/internal/store"
)

// inSendWindow reports whether now (in the campaign timezone) falls within the
// campaign's configured sending hours and weekdays.
func inSendWindow(c store.Campaign, now time.Time) bool {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc, _ = time.LoadLocation("America/New_York")
	}
	local := now.In(loc)
	hour := local.Hour()
	if hour < c.SendStartHour || hour >= c.SendEndHour {
		return false
	}
	wd := int(local.Weekday()) // 0=Sun..6=Sat
	for _, d := range c.SendWeekdays {
		if int(d) == wd {
			return true
		}
	}
	return false
}

// nextWindowOpen returns the next time the campaign window opens after now.
func nextWindowOpen(c store.Campaign, now time.Time) time.Time {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc, _ = time.LoadLocation("America/New_York")
	}
	local := now.In(loc)
	for i := 0; i < 14; i++ {
		day := local.AddDate(0, 0, i)
		candidate := time.Date(day.Year(), day.Month(), day.Day(), c.SendStartHour, 0, 0, 0, loc)
		if candidate.After(now) {
			wd := int(candidate.Weekday())
			for _, d := range c.SendWeekdays {
				if int(d) == wd {
					return candidate
				}
			}
		}
	}
	return now.Add(1 * time.Hour)
}

// InSendWindow reports whether now falls inside the campaign's send window
// (exported for the send-status diagnosis endpoint).
func InSendWindow(c store.Campaign, now time.Time) bool { return inSendWindow(c, now) }

// NextWindowOpen returns when the campaign's send window next opens.
func NextWindowOpen(c store.Campaign, now time.Time) time.Time { return nextWindowOpen(c, now) }
