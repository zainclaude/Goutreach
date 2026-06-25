package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/zainclaude/goutreach/internal/store"
	"github.com/zainclaude/goutreach/internal/verify"
)

const (
	keyVerifyProvider = "email_verify_provider" // millionverifier|zerobounce|neverbounce|bouncer
	keyVerifyAPIKey   = "email_verify_api_key"  // secret
)

// buildVerifier constructs the configured email verifier for a user, or an error
// if no provider/key is set.
func (s *Server) buildVerifier(ctx context.Context, userID int64) (verify.Verifier, error) {
	provider, _, _ := s.st.GetSetting(ctx, userID, keyVerifyProvider)
	enc, ok, _ := s.st.GetSetting(ctx, userID, keyVerifyAPIKey)
	if !ok || enc == "" {
		return nil, fmt.Errorf("no verification API key set — add one in Settings")
	}
	key, err := s.cipher.Decrypt(enc)
	if err != nil {
		return nil, err
	}
	return verify.New(provider, key)
}

// handleVerifyLeads verifies all not-yet-verified leads in the background and
// returns how many were queued.
func (s *Server) handleVerifyLeads(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	v, err := s.buildVerifier(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	leads, err := s.st.ListUnverifiedLeads(r.Context(), userID, 10000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	go s.runVerification(v, leads)
	writeJSON(w, http.StatusOK, map[string]int{"queued": len(leads)})
}

// verifyUnverifiedAsync kicks off background verification of a user's unverified
// leads if a verifier is configured (best-effort, e.g. after a CSV import).
func (s *Server) verifyUnverifiedAsync(userID int64) {
	v, err := s.buildVerifier(context.Background(), userID)
	if err != nil {
		return // not configured — silently skip
	}
	leads, err := s.st.ListUnverifiedLeads(context.Background(), userID, 10000)
	if err != nil || len(leads) == 0 {
		return
	}
	go s.runVerification(v, leads)
}

// runVerification verifies each lead sequentially (gentle on provider rate
// limits) and records the result. Runs detached from the request.
func (s *Server) runVerification(v verify.Verifier, leads []store.Lead) {
	ctx := context.Background()
	ok, bad := 0, 0
	for _, l := range leads {
		status, _, err := v.Verify(ctx, l.Email)
		if err != nil {
			s.log.Printf("verify: lead %d (%s): %v", l.ID, l.Email, err)
			continue
		}
		if err := s.st.SetLeadVerification(ctx, l.ID, status); err != nil {
			s.log.Printf("verify: save lead %d: %v", l.ID, err)
			continue
		}
		if status == verify.StatusInvalid {
			bad++
		} else {
			ok++
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.log.Printf("verify: finished %d leads (%d invalid)", ok+bad, bad)
}
