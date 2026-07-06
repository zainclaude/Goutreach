package api

import (
	"net/http"
	"time"

	"github.com/zainclaude/goutreach/internal/auth"
)

type credsReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResp struct {
	Token string `json:"token"`
	Email string `json:"email"`
}

// handleSignup creates the first (and only, in MVP) user.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req credsReq
	if err := readJSON(r, &req); err != nil || req.Email == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "email and password required")
		return
	}
	n, err := s.st.CountUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n > 0 {
		writeErr(w, http.StatusForbidden, "signup is closed (a user already exists)")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, err := s.st.CreateUser(r.Context(), trimLower(req.Email), hash)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	tok, _ := s.auth.IssueToken(u.ID)
	writeJSON(w, http.StatusOK, tokenResp{Token: tok, Email: u.Email})
}

// handleLogin authenticates a user.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credsReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u, err := s.st.GetUserByEmail(r.Context(), trimLower(req.Email))
	if err != nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	tok, _ := s.auth.IssueToken(u.ID)
	writeJSON(w, http.StatusOK, tokenResp{Token: tok, Email: u.Email})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        s.userID(r),
		"ai_enabled":     s.gen.Enabled(),
		"google_enabled": s.google.Enabled(),
	})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	// ?range=today|3d|7d|all restricts the stats window (default all time).
	var since *time.Time
	now := time.Now().UTC()
	switch r.URL.Query().Get("range") {
	case "today":
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		since = &t
	case "3d":
		t := now.Add(-3 * 24 * time.Hour)
		since = &t
	case "7d":
		t := now.Add(-7 * 24 * time.Hour)
		since = &t
	}
	stats, err := s.st.OverviewStats(r.Context(), s.userID(r), since)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
