package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/zainclaude/goutreach/internal/beehiiv"
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

// handleListSent returns delivered campaign emails (warmup excluded) so the
// user can see what was actually generated and sent.
func (s *Server) handleListSent(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.st.ListSentMessages(r.Context(), s.userID(r), 500)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// handleBeehiivSync subscribes every interested replier to the configured
// beehiiv newsletter. Idempotent (beehiiv upserts), so it doubles as a
// key-validity test and a backfill for replies from before the integration.
func (s *Server) handleBeehiivSync(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	pubID, _, _ := s.st.GetSetting(r.Context(), userID, "beehiiv_publication_id")
	enc, _, _ := s.st.GetSetting(r.Context(), userID, "beehiiv_api_key")
	if strings.TrimSpace(pubID) == "" || enc == "" {
		writeErr(w, http.StatusBadRequest, "add your beehiiv API key and publication ID first")
		return
	}
	apiKey, err := s.cipher.Decrypt(enc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "decrypt beehiiv key: "+err.Error())
		return
	}
	emails, err := s.st.ListInterestedLeadEmails(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	added, errs := 0, []string{}
	for _, e := range emails {
		if err := beehiiv.Subscribe(r.Context(), apiKey, pubID, e); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", e, err))
			s.log.Printf("beehiiv sync: %s: %v", e, err)
			continue
		}
		added++
	}
	writeJSON(w, http.StatusOK, map[string]any{"synced": added, "total": len(emails), "errors": errs})
}

// handleSendReply sends a manual reply in the thread of a replied message, from
// the same inbox that originally sent it.
func (s *Server) handleSendReply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
		Cc   string `json:"cc"` // comma/space/semicolon separated addresses
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	var cc []string
	for _, addr := range strings.FieldsFunc(req.Cc, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	}) {
		if addr = strings.TrimSpace(addr); addr != "" {
			cc = append(cc, addr)
		}
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
		Cc:        cc,
		Subject:   subject,
		TextBody:  req.Body,
		InReplyTo: msg.MessageID,
		Refs:      refs,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "send failed: "+err.Error())
		return
	}
	// The conversation is now live — from here on, every inbound reply in this
	// thread triggers a notification email so a booking never slips by.
	if err := s.st.SetMessageUserReplied(r.Context(), msg.ID); err != nil {
		s.log.Printf("reply: mark user-replied msg %d: %v", msg.ID, err)
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
