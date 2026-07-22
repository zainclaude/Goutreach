package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/zainclaude/goutreach/internal/store"
)

type campaignReq struct {
	Name            string  `json:"name"`
	Brief           string  `json:"brief"`
	Timezone        string  `json:"timezone"`
	SendStartHour   int     `json:"send_start_hour"`
	SendEndHour     int     `json:"send_end_hour"`
	SendWeekdays    []int32 `json:"send_weekdays"`
	DailyCap        int     `json:"daily_cap"`
	TrackOpens      bool    `json:"track_opens"`
	TrackClicks     bool    `json:"track_clicks"`
	RequireApproval bool    `json:"require_approval"`
	ApprovalCount   int     `json:"approval_count"`
	Steps           []struct {
		DelayDays int    `json:"delay_days"`
		Angle     string `json:"angle"`
	} `json:"steps"`
}

func (s *Server) handleCreateCampaign(w http.ResponseWriter, r *http.Request) {
	var req campaignReq
	if err := readJSON(r, &req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	if req.Timezone == "" {
		req.Timezone = "America/New_York" // EST/EDT by default
	}
	if req.SendEndHour == 0 {
		req.SendEndHour = 17
	}
	if req.SendStartHour == 0 {
		req.SendStartHour = 9
	}
	if req.DailyCap == 0 {
		req.DailyCap = 100
	}
	if req.ApprovalCount == 0 {
		req.ApprovalCount = 5
	}
	c, err := s.st.CreateCampaign(r.Context(), store.Campaign{
		UserID:          s.userID(r),
		Name:            req.Name,
		Brief:           req.Brief,
		Timezone:        req.Timezone,
		SendStartHour:   req.SendStartHour,
		SendEndHour:     req.SendEndHour,
		SendWeekdays:    req.SendWeekdays,
		DailyCap:        req.DailyCap,
		TrackOpens:      req.TrackOpens,
		TrackClicks:     req.TrackClicks,
		RequireApproval: req.RequireApproval,
		ApprovalCount:   req.ApprovalCount,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Persist steps (default a single step 0 if none supplied).
	var steps []store.CampaignStep
	if len(req.Steps) == 0 {
		steps = []store.CampaignStep{{CampaignID: c.ID, StepIndex: 0, DelayDays: 0}}
	} else {
		for i, st := range req.Steps {
			steps = append(steps, store.CampaignStep{
				CampaignID: c.ID, StepIndex: i, DelayDays: st.DelayDays, Angle: st.Angle,
			})
		}
	}
	if err := s.st.ReplaceSteps(r.Context(), c.ID, steps); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleListCampaigns(w http.ResponseWriter, r *http.Request) {
	cs, err := s.st.ListCampaigns(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *Server) handleGetCampaign(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetCampaign(r.Context(), s.userID(r), idParam(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	steps, _ := s.st.ListSteps(r.Context(), c.ID)
	accIDs, _ := s.st.ListCampaignAccountIDs(r.Context(), c.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"campaign": c, "steps": steps, "account_ids": accIDs,
	})
}

func (s *Server) handleUpdateCampaignStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := readJSON(r, &req); err != nil || req.Status == "" {
		writeErr(w, http.StatusBadRequest, "status required")
		return
	}
	switch req.Status {
	case "draft", "running", "paused":
	default:
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err := s.st.SetCampaignStatus(r.Context(), s.userID(r), idParam(r), req.Status); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleDeleteCampaign deletes a campaign with all its enrollments, generated
// messages, and stats. Already-sent mail is unaffected; leads stay in the pool.
func (s *Server) handleDeleteCampaign(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteCampaign(r.Context(), s.userID(r), idParam(r)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetSteps(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if _, err := s.st.GetCampaign(r.Context(), s.userID(r), id); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Steps []struct {
			DelayDays int    `json:"delay_days"`
			Angle     string `json:"angle"`
		} `json:"steps"`
	}
	if err := readJSON(r, &req); err != nil || len(req.Steps) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one step required")
		return
	}
	var steps []store.CampaignStep
	for i, st := range req.Steps {
		steps = append(steps, store.CampaignStep{CampaignID: id, StepIndex: i, DelayDays: st.DelayDays, Angle: st.Angle})
	}
	if err := s.st.ReplaceSteps(r.Context(), id, steps); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetCampaignAccounts(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if _, err := s.st.GetCampaign(r.Context(), s.userID(r), id); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.st.SetCampaignAccounts(r.Context(), id, req.AccountIDs); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if _, err := s.st.GetCampaign(r.Context(), s.userID(r), id); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		LeadIDs   []int64 `json:"lead_ids"`
		All       bool    `json:"all"`
		Unemailed bool    `json:"unemailed"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	ids := req.LeadIDs
	if req.All || req.Unemailed {
		// "all" enrolls every lead; "unemailed" only those never actually sent to
		// (previewed-but-unsent leads still count as unemailed).
		list := s.st.ListLeads
		if req.Unemailed {
			list = s.st.ListUnemailedLeads
		}
		leads, err := list(r.Context(), s.userID(r))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		ids = ids[:0]
		for _, l := range leads {
			ids = append(ids, l.ID)
		}
	}
	added, err := s.st.EnrollLeads(r.Context(), id, ids)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// New leads in a finished campaign resume sending: completed -> running.
	if added > 0 {
		_ = s.st.ReactivateIfCompleted(r.Context(), s.userID(r), id)
	}
	writeJSON(w, http.StatusOK, map[string]int{"enrolled": added})
}

// handleCampaignLeads lists the leads currently enrolled in a campaign.
func (s *Server) handleCampaignLeads(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if _, err := s.st.GetCampaign(r.Context(), s.userID(r), id); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	leads, err := s.st.ListCampaignLeadDetails(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, leads)
}

// handleUnenrollLead removes one lead from a campaign (and its draft message).
func (s *Server) handleUnenrollLead(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if _, err := s.st.GetCampaign(r.Context(), s.userID(r), id); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	leadID := parseInt64(chi.URLParam(r, "leadID"))
	removed, err := s.st.UnenrollLead(r.Context(), id, leadID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
}

// handleUnenrollAll removes every lead from a campaign (and their draft messages).
func (s *Server) handleUnenrollAll(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	if _, err := s.st.GetCampaign(r.Context(), s.userID(r), id); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	removed, err := s.st.UnenrollAllLeads(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
}

// handlePreview generates (without sending) the first N step-0 emails so the
// user can review them before launching. Generation runs in the background;
// poll GET /campaigns/{id}/messages to see results.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	campaign, err := s.st.GetCampaign(r.Context(), s.userID(r), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if !s.gen.Enabled() {
		writeErr(w, http.StatusBadRequest, "AI generation disabled: set ANTHROPIC_API_KEY")
		return
	}
	count := campaign.ApprovalCount
	if c := r.URL.Query().Get("count"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			count = n
		}
	}
	if count > 25 {
		count = 25
	}
	leads, err := s.st.ListActiveCampaignLeads(r.Context(), id, count)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(leads) == 0 {
		writeErr(w, http.StatusBadRequest, "no enrolled leads to preview; enroll leads first")
		return
	}

	// Pre-create the batch's message rows up front. Generation runs lazily one
	// lead at a time, so without this a restart mid-batch leaves the untouched
	// leads with no rows at all — invisible to the recovery sweep. With rows
	// pre-created, every interrupted or unstarted lead self-heals.
	for _, cl := range leads {
		if _, err := s.st.GetMessageForStep(r.Context(), cl.ID, 0); err != nil {
			if _, err := s.st.CreateMessage(r.Context(), store.Message{
				CampaignLeadID: cl.ID,
				StepIndex:      0,
				Status:         "queued",
				Approved:       !campaign.RequireApproval,
			}); err != nil {
				s.log.Printf("preview: pre-create msg for lead %d: %v", cl.LeadID, err)
			}
		}
	}

	// Generate in the background (each can take a while due to web research).
	go func(cl []store.CampaignLead, camp store.Campaign) {
		for _, lead := range cl {
			ctx, cancel := context.WithTimeout(context.Background(), 6*60_000_000_000) // 6m
			if _, err := s.sender.GeneratePreview(ctx, camp, lead, 0); err != nil {
				s.log.Printf("preview gen lead %d: %v", lead.ID, err)
			}
			cancel()
		}
	}(leads, campaign)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"generating": len(leads),
		"message":    "Previews are generating; poll the messages endpoint.",
	})
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if err := s.st.LaunchCampaign(r.Context(), s.userID(r), idParam(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCampaignMessages(w http.ResponseWriter, r *http.Request) {
	id := idParam(r)
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	msgs, err := s.st.ListMessagesForCampaign(r.Context(), s.userID(r), id, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleCampaignStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.st.CampaignStatsFor(r.Context(), idParam(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
