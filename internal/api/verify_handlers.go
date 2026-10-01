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

	// verifyBatchSize bounds one fetch, not the run: runVerification keeps
	// fetching the next batch until no unverified leads remain.
	verifyBatchSize = 10000
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
	leads, err := s.st.ListUnverifiedLeads(r.Context(), userID, verifyBatchSize)
	if err != nil {
		verifyDone(userID)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The run works through ALL unverified leads batch by batch, so report the
	// full total, not just the first batch.
	total, err := s.st.CountUnverifiedLeads(r.Context(), userID)
	if err != nil {
		total = len(leads)
	}
	// Nothing new to verify? An explicit click retries leads the provider
	// answered 'unknown' for — often transient (timeouts, greylisting).
	// Automatic runs never touch these, so they can't loop.
	retrying := false
	if len(leads) == 0 {
		leads, err = s.st.ListUnknownResultLeads(r.Context(), userID, verifyBatchSize)
		if err != nil {
			verifyDone(userID)
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		total = len(leads)
		retrying = true
	}
	if len(leads) == 0 {
		verifyDone(userID)
		writeJSON(w, http.StatusOK, map[string]any{"queued": 0, "retrying": false})
		return
	}
	if retrying {
		s.log.Printf("verify: starting — retrying %d unknown-result lead(s)", total)
	} else {
		s.log.Printf("verify: starting — %d unverified lead(s) queued (batches of %d)", total, verifyBatchSize)
	}
	go s.runVerification(userID, v, leads)
	writeJSON(w, http.StatusOK, map[string]any{"queued": total, "retrying": retrying})
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
	leads, err := s.st.ListUnverifiedLeads(context.Background(), userID, verifyBatchSize)
	if err != nil || len(leads) == 0 {
		verifyDone(userID)
		return
	}
	go s.runVerification(userID, v, leads)
}

// ResumeVerification restarts verification runs that a deploy killed: shortly
// after boot, any user with a configured verifier and leads still awaiting
// verification picks up where the run left off.
func (s *Server) ResumeVerification(ctx context.Context) {
	select {
	case <-time.After(30 * time.Second): // let the app settle first
	case <-ctx.Done():
		return
	}
	userIDs, err := s.st.UserIDsWithUnverifiedLeads(ctx)
	if err != nil {
		s.log.Printf("verify resume: %v", err)
		return
	}
	for _, uid := range userIDs {
		s.log.Printf("verify resume: user %d has unverified leads — resuming", uid)
		s.verifyUnverifiedAsync(uid)
	}
}

// runVerification verifies each lead sequentially, paced to stay under provider
// rate limits, and records the result. Runs detached from the request, working
// through every unverified lead in batches of verifyBatchSize.
func (s *Server) runVerification(userID int64, v verify.Verifier, leads []store.Lead) {
	defer verifyDone(userID)
	ctx := context.Background()
	counts := map[string]int{}
	errs := 0
batches:
	for batch := 1; len(leads) > 0; batch++ {
		saved := 0
		for _, l := range leads {
			time.Sleep(1 * time.Second) // ~1 req/s — well under provider rate limits
			status, _, err := v.Verify(ctx, l.Email)
			if err != nil {
				if verify.IsOutOfCredits(err) {
					s.log.Printf("verify: stopping — out of credits")
					break batches
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
			saved++
		}
		// A batch where nothing got recorded means every lead is failing (bad
		// key, provider outage) — refetching would just spin on the same leads.
		if saved == 0 {
			s.log.Printf("verify: batch %d recorded no results (%d errors) — stopping", batch, errs)
			break
		}
		var err error
		leads, err = s.st.ListUnverifiedLeads(ctx, userID, verifyBatchSize)
		if err != nil {
			s.log.Printf("verify: fetching next batch: %v", err)
			break
		}
		if len(leads) > 0 {
			s.log.Printf("verify: batch %d done — %d unverified lead(s) remain, continuing", batch, len(leads))
		}
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
