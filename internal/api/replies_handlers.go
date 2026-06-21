package api

import (
	"net/http"
	"strings"

	"github.com/zainclaude/goutreach/internal/mailer"
)

// handleListReplies returns the unified inbox of lead replies (warmup excluded).
func (s *Server) handleListReplies(w http.ResponseWriter, r *http.Request) {
	replies, err := s.st.ListReplies(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, replies)
}

// handleSendReply sends a manual reply in the thread of a replied message, from
// the same inbox that originally sent it.
func (s *Server) handleSendReply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	msg, acc, lead, ok, err := s.st.GetReplyContext(r.Context(), s.userID(r), idParam(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "thread not found")
		return
	}
	creds, err := s.res.SMTP(r.Context(), acc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "smtp creds: "+err.Error())
		return
	}
	subject := msg.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	refs := strings.TrimSpace(msg.References + " " + msg.MessageID)
	_, err = mailer.Send(r.Context(), creds, mailer.OutgoingEmail{
		FromAddr:  acc.Email,
		FromName:  acc.FromName,
		ToAddr:    lead.Email,
		ToName:    strings.TrimSpace(lead.FirstName + " " + lead.LastName),
		Subject:   subject,
		TextBody:  req.Body,
		InReplyTo: msg.MessageID,
		Refs:      refs,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "send failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAccountStats returns per-inbox engagement metrics.
func (s *Server) handleAccountStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.st.AccountStats(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
