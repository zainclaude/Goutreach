package sender

import (
	"time"

	"github.com/zainclaude/goutreach/internal/store"
)

// inSendWindow reports whether now (in the campaign timezone) falls within the
// campaign's configured sending hours and weekdays.
func inSendWindow(c store.Campaign, now time.Time) bool {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
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
		loc = time.UTC
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
