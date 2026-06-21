package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zainclaude/goutreach/internal/store"
)

// --- messages ---

func (s *Server) handleApproveMessage(w http.ResponseWriter, r *http.Request) {
	if err := s.st.SetMessageApproved(r.Context(), idParam(r), true); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRejectMessage(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteMessage(r.Context(), idParam(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.st.UpdateMessageContent(r.Context(), idParam(r), req.Subject, req.Body); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- templates ---

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	ts, err := s.st.ListTemplates(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (s *Server) handleSetTemplate(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	valid := false
	for _, k := range store.TemplateKeys {
		if k == key {
			valid = true
		}
	}
	if !valid {
		writeErr(w, http.StatusBadRequest, "invalid template key")
		return
	}
	var req struct {
		Name    string `json:"name"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.st.UpsertTemplate(r.Context(), s.userID(r), store.EmailTemplate{
		Key: key, Name: req.Name, Subject: req.Subject, Body: req.Body,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- settings ---

func (s *Server) handleListSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.st.ListSettingKeys(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleSetSetting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}
	if err := readJSON(r, &req); err != nil || req.Key == "" {
		writeErr(w, http.StatusBadRequest, "key required")
		return
	}
	value := req.Value
	if req.IsSecret && value != "" {
		enc, err := s.cipher.Encrypt(value)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		value = enc
	}
	if err := s.st.SetSetting(r.Context(), s.userID(r), req.Key, value, req.IsSecret); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
