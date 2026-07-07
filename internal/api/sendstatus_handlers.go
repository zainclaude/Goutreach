package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zainclaude/goutreach/internal/sender"
)

// handleCampaignSendStatus explains whether a campaign can send right now and,
// if not, exactly what is blocking it — so "why didn't it send?" is answerable
// from the UI instead of the server logs.
func (s *Server) handleCampaignSendStatus(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	c, err := s.st.GetCampaign(r.Context(), userID, idParam(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	now := time.Now()
	blockers, notes := []string{}, []string{} // non-nil so JSON is [], never null

	counts, err := s.st.CampaignLeadStates(r.Context(), c.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 1. Campaign state gates.
	switch {
	case c.Status == "completed":
		blockers = append(blockers, "Campaign is completed — every lead finished, replied, or was skipped. Enroll more leads to resume.")
	case c.Status != "running":
		blockers = append(blockers, fmt.Sprintf("Campaign status is '%s' — nothing sends until it's running. Use 'Approve & launch' (or 'Set running').", c.Status))
	}
	if c.RequireApproval {
		blockers = append(blockers, "Approval mode is on: the tool only generates previews. Click 'Approve & launch' to start sending.")
	}

	// 2. Leads.
	switch {
	case counts.Total == 0:
		blockers = append(blockers, "No leads are enrolled in this campaign.")
	case counts.Active == 0:
		blockers = append(blockers, fmt.Sprintf("No sendable leads left: %d skipped, %d finished, %d replied, %d bounced.",
			counts.Skipped, counts.Finished, counts.Replied, counts.Bounced))
	default:
		if s.requireVerifiedSend(r, userID) && counts.Unverified > 0 {
			msg := fmt.Sprintf("%d of %d active leads are held awaiting email verification — run 'Verify emails' on the Leads page.", counts.Unverified, counts.Active)
			if counts.Unverified == counts.Active {
				blockers = append(blockers, msg)
			} else {
				notes = append(notes, msg)
			}
		}
		if counts.BadEmail > 0 {
			notes = append(notes, fmt.Sprintf("%d active lead(s) verified invalid/risky and will be skipped at send time.", counts.BadEmail))
		}
	}

	// 3. Campaign daily cap.
	if c.DailyCap > 0 {
		if sentToday, err := s.st.CountCampaignSentToday(r.Context(), c.ID); err == nil {
			if sentToday >= c.DailyCap {
				blockers = append(blockers, fmt.Sprintf("Daily cap reached (%d/%d sent today) — sending resumes tomorrow.", sentToday, c.DailyCap))
			} else {
				notes = append(notes, fmt.Sprintf("Daily cap: %d of %d used today.", sentToday, c.DailyCap))
			}
		}
	}

	// 4. Send window.
	if !sender.InSendWindow(c, now) {
		next := sender.NextWindowOpen(c, now)
		blockers = append(blockers, fmt.Sprintf("Outside the send window (%02d:00–%02d:00 %s, days %s). Sending resumes %s.",
			c.SendStartHour, c.SendEndHour, c.Timezone, weekdayList(c.SendWeekdays), next.Format("Mon Jan 2 15:04 MST")))
	}

	// 5. Inboxes.
	ids, err := s.st.ListCampaignAccountIDs(r.Context(), c.ID)
	if err == nil {
		if len(ids) == 0 {
			blockers = append(blockers, "No sending inboxes are selected for this campaign.")
		} else {
			eligible, inactive, capped := 0, 0, 0
			for _, id := range ids {
				acc, err := s.st.GetAccountByID(r.Context(), id)
				if err != nil || acc.Status != "active" {
					inactive++
					continue
				}
				if sent, err := s.st.CountSentToday(r.Context(), acc.ID); err == nil && sent >= acc.DailyLimit {
					capped++
					continue
				}
				eligible++
			}
			if eligible == 0 {
				blockers = append(blockers, fmt.Sprintf("No eligible inbox: of %d selected, %d aren't active/verified and %d hit their daily cap.", len(ids), inactive, capped))
			} else if inactive > 0 || capped > 0 {
				notes = append(notes, fmt.Sprintf("%d of %d inboxes eligible (%d inactive, %d at daily cap).", eligible, len(ids), inactive, capped))
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sending":  len(blockers) == 0,
		"blockers": blockers,
		"notes":    notes,
		"counts":   counts,
	})
}

// requireVerifiedSend mirrors the sender's gate: default on unless the setting
// is explicitly disabled.
func (s *Server) requireVerifiedSend(r *http.Request, userID int64) bool {
	v, ok, _ := s.st.GetSetting(r.Context(), userID, "require_verified_send")
	if !ok {
		return true
	}
	return v != "0" && strings.ToLower(v) != "false"
}

func weekdayList(days []int32) string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	var out []string
	for _, d := range days {
		if d >= 0 && d < 7 {
			out = append(out, names[d])
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ",")
}
