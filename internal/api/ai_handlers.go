package api

import (
	"net/http"
	"strings"

	"github.com/zainclaude/goutreach/internal/ai"
	"github.com/zainclaude/goutreach/internal/search"
)

// aiSecret loads and decrypts an encrypted setting, distinguishing "not saved"
// from "cannot decrypt" so the Settings test buttons show a useful error.
func (s *Server) aiSecret(r *http.Request, key string) (string, int, string) {
	enc, ok, _ := s.st.GetSetting(r.Context(), s.userID(r), key)
	if !ok || strings.TrimSpace(enc) == "" {
		return "", http.StatusBadRequest, "no key saved yet — save it first"
	}
	val, err := s.cipher.Decrypt(enc)
	if err != nil {
		return "", http.StatusInternalServerError, "could not decrypt stored key: " + err.Error()
	}
	return val, 0, ""
}

// handleKimiPing verifies the stored Moonshot API key (and model name) with a
// minimal live generation request, so key/model problems are diagnosable from
// Settings before switching the provider over.
func (s *Server) handleKimiPing(w http.ResponseWriter, r *http.Request) {
	key, code, msg := s.aiSecret(r, ai.SettingKimiAPIKey)
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	model, _, _ := s.st.GetSetting(r.Context(), s.userID(r), ai.SettingKimiModel)
	reply, usedModel, err := ai.VerifyKimiKey(r.Context(), key, model)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Moonshot rejected the request: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": usedModel, "reply": reply})
}

// handleSerperPing verifies the stored Serper.dev key with a real (1-credit)
// search, since that's exactly what the Kimi research path will do.
func (s *Server) handleSerperPing(w http.ResponseWriter, r *http.Request) {
	key, code, msg := s.aiSecret(r, ai.SettingSerperAPIKey)
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	out, err := search.Serper(r.Context(), key, "TikTok Shop brands", 3)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Serper rejected the request: "+err.Error())
		return
	}
	sample := out
	if i := strings.IndexByte(sample, '\n'); i > 0 {
		sample = sample[:i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sample": strings.TrimSpace(sample)})
}
