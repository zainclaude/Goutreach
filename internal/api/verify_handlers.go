package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zainclaude/goutreach/internal/store"
	"github.com/zainclaude/goutreach/internal/verify"
)

const (
	keyVerifyProvider = "email_verify_provider" // millionverifier|zerobounce|neverbounce|bouncer
	keyVerifyAPIKey   = "email_verify_api_key"  // secret
)

// verifyRunning guards against stacking multiple verification passes for the same
// user — concurrent runs are what trip the provider's rate limit.
var (
	verifyMu      sync.Mutex
	verifyRunning = map[int64]bool{}
)

func verifyTryStart(userID int64) bool {
	verifyMu.Lock()
	defer verifyMu.Unlock()
	if verifyRunning[userID] {
		return false
	}
	verifyRunning[userID] = true
	return true
}

func verifyDone(userID int64) {
	verifyMu.Lock()
	delete(verifyRunning, userID)
	verifyMu.Unlock()
}

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
		s.log.Printf("verify: cannot start — %v", err)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Pre-flight (e.g. credit balance) so problems surface immediately in the UI.
	if err := v.Preflight(r.Context()); err != nil {
		s.log.Printf("verify: preflight failed — %v", err)
		writeErr(w, http.StatusBadRequest, friendlyVerifyErr(err))
		return
	}
	if !verifyTryStart(userID) {
		writeErr(w, http.StatusConflict, "a verification run is already in progress — let it finish before starting another")
		return
	}
	leads, err := s.st.ListUnverifiedLeads(r.Context(), userID, 10000)
	if err != nil {
		verifyDone(userID)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.log.Printf("verify: starting — %d unverified lead(s) queued", len(leads))
	go s.runVerification(userID, v, leads)
	writeJSON(w, http.StatusOK, map[string]int{"queued": len(leads)})
}

// verifyUnverifiedAsync kicks off background verification of a user's unverified
// leads if a verifier is configured (best-effort, e.g. after a CSV import).
func (s *Server) verifyUnverifiedAsync(userID int64) {
	v, err := s.buildVerifier(context.Background(), userID)
	if err != nil {
		return // not configured — silently skip
	}
	if !verifyTryStart(userID) {
		return // a run is already going; it will pick these up
	}
	leads, err := s.st.ListUnverifiedLeads(context.Background(), userID, 10000)
	if err != nil || len(leads) == 0 {
		verifyDone(userID)
		return
	}
	go s.runVerification(userID, v, leads)
}

// runVerification verifies each lead sequentially, paced to stay under provider
// rate limits, and records the result. Runs detached from the request.
func (s *Server) runVerification(userID int64, v verify.Verifier, leads []store.Lead) {
	defer verifyDone(userID)
	ctx := context.Background()
	counts := map[string]int{}
	errs := 0
	for _, l := range leads {
		time.Sleep(1 * time.Second) // ~1 req/s — well under provider rate limits
		status, _, err := v.Verify(ctx, l.Email)
		if err != nil {
			if verify.IsOutOfCredits(err) {
				s.log.Printf("verify: stopping — out of credits")
				break
			}
			errs++
			if errs <= 3 { // avoid log spam if the key/plan is bad for every lead
				s.log.Printf("verify: lead %d (%s): %v", l.ID, l.Email, err)
			}
			continue
		}
		if err := s.st.SetLeadVerification(ctx, l.ID, status); err != nil {
			s.log.Printf("verify: save lead %d: %v", l.ID, err)
			continue
		}
		counts[status]++
	}
	s.log.Printf("verify: finished — valid=%d invalid=%d risky=%d catch_all=%d unknown=%d errors=%d",
		counts[verify.StatusValid], counts[verify.StatusInvalid], counts[verify.StatusRisky],
		counts[verify.StatusCatchAll], counts[verify.StatusUnknown], errs)
}

// friendlyVerifyErr rewrites a raw rate-limit error into an actionable message.
func friendlyVerifyErr(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "429") || strings.Contains(strings.ToLower(msg), "rate") {
		return "ZeroBounce is rate-limiting your account right now (too many verification requests). Wait ~2 minutes, then try again — verification now runs slower to avoid this."
	}
	return msg
}
