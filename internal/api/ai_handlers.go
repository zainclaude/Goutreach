package api

import (
	"fmt"
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
		errMsg := "Moonshot rejected the request: " + err.Error()
		// A model-not-found means the key itself works — list the model IDs the
		// key can actually use so the right one can be picked from Settings.
		if strings.Contains(err.Error(), "resource_not_found") || strings.Contains(err.Error(), "Not found the model") {
			if models, mErr := ai.ListKimiModels(r.Context(), key); mErr == nil && len(models) > 0 {
				errMsg = fmt.Sprintf("Your key works, but model %q doesn't exist on Moonshot. Models your key can use: %s — paste one into the Kimi model field and re-test.",
					usedModel, strings.Join(models, ", "))
			}
		}
		writeErr(w, http.StatusBadGateway, errMsg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "model": usedModel, "reply": reply})
}

// handleKimiModels lists the model IDs the stored Moonshot key has access to.
func (s *Server) handleKimiModels(w http.ResponseWriter, r *http.Request) {
	key, code, msg := s.aiSecret(r, ai.SettingKimiAPIKey)
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	models, err := ai.ListKimiModels(r.Context(), key)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Moonshot models list failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "models": models})
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
