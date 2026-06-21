// Package api wires HTTP handlers for the dashboard and tracking endpoints.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/zainclaude/goutreach/internal/ai"
	"github.com/zainclaude/goutreach/internal/auth"
	"github.com/zainclaude/goutreach/internal/config"
	"github.com/zainclaude/goutreach/internal/crypto"
	"github.com/zainclaude/goutreach/internal/googleoauth"
	"github.com/zainclaude/goutreach/internal/mailauth"
	"github.com/zainclaude/goutreach/internal/sender"
	"github.com/zainclaude/goutreach/internal/store"
	"github.com/zainclaude/goutreach/internal/tracking"
)

// Server holds dependencies for all HTTP handlers.
type Server struct {
	cfg      config.Config
	st       *store.Store
	auth     *auth.Auth
	cipher   *crypto.Cipher
	res      *mailauth.Resolver
	google   *googleoauth.Client
	sender   *sender.Service
	gen      *ai.Generator
	tracking *tracking.Handlers
	log      *log.Logger
}

// New builds a Server.
func New(cfg config.Config, st *store.Store, a *auth.Auth, cipher *crypto.Cipher, res *mailauth.Resolver, google *googleoauth.Client, snd *sender.Service, gen *ai.Generator, logger *log.Logger) *Server {
	return &Server{
		cfg: cfg, st: st, auth: a, cipher: cipher, res: res, google: google,
		sender: snd, gen: gen, tracking: tracking.NewHandlers(st), log: logger,
	}
}

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })

	// Public tracking endpoints.
	r.Get("/t/open/*", s.tracking.Open)
	r.Get("/t/click/*", s.tracking.Click)

	// Public auth.
	r.Post("/api/auth/signup", s.handleSignup)
	r.Post("/api/auth/login", s.handleLogin)

	// OAuth callback is public (Google redirects here without a bearer token).
	r.Get("/api/oauth/google/callback", s.handleGoogleCallback)

	// Protected API.
	r.Route("/api", func(r chi.Router) {
		r.Use(s.auth.Middleware)

		r.Get("/me", s.handleMe)
		r.Get("/overview", s.handleOverview)

		r.Route("/accounts", func(r chi.Router) {
			r.Get("/", s.handleListAccounts)
			r.Get("/stats", s.handleAccountStats)
			r.Post("/", s.handleCreateAccount)
			r.Post("/import", s.handleImportAccounts)
			r.Post("/verify", s.handleVerifyAccount)
			r.Patch("/{id}", s.handleUpdateAccount)
			r.Delete("/{id}", s.handleDeleteAccount)
		})

		r.Route("/domains", func(r chi.Router) {
			r.Get("/", s.handleListDomains)
			r.Get("/ping", s.handlePorkbunPing)
			r.Get("/check", s.handleCheckDomain)
			r.Post("/register", s.handleRegisterDomain)
			r.Post("/import", s.handleImportDomain)
			r.Post("/{id}/dns", s.handleApplyDNS)
			r.Delete("/{id}", s.handleDeleteDomain)
		})

		r.Route("/maildoso", func(r chi.Router) {
			r.Get("/ping", s.handleMaildosoPing)
			r.Get("/domains", s.handleMaildosoListDomains)
			r.Post("/domains", s.handleMaildosoCreateDomain)
			r.Post("/mailboxes", s.handleMaildosoOrderMailboxes)
			r.Post("/sync", s.handleMaildosoSync)
		})

		// Returns the Google consent URL to redirect the browser to.
		r.Get("/oauth/google/start", s.handleGoogleStart)

		r.Route("/replies", func(r chi.Router) {
			r.Get("/", s.handleListReplies)
			r.Post("/{id}/reply", s.handleSendReply)
		})

		r.Route("/leads", func(r chi.Router) {
			r.Get("/", s.handleListLeads)
			r.Post("/", s.handleCreateLead)
			r.Post("/import", s.handleImportLeads)
			r.Delete("/{id}", s.handleDeleteLead)
		})

		r.Route("/campaigns", func(r chi.Router) {
			r.Get("/", s.handleListCampaigns)
			r.Post("/", s.handleCreateCampaign)
			r.Get("/{id}", s.handleGetCampaign)
			r.Patch("/{id}", s.handleUpdateCampaignStatus)
			r.Put("/{id}/steps", s.handleSetSteps)
			r.Put("/{id}/accounts", s.handleSetCampaignAccounts)
			r.Post("/{id}/enroll", s.handleEnroll)
			r.Post("/{id}/preview", s.handlePreview)
			r.Post("/{id}/launch", s.handleLaunch)
			r.Get("/{id}/messages", s.handleCampaignMessages)
			r.Get("/{id}/stats", s.handleCampaignStats)
		})

		r.Route("/messages", func(r chi.Router) {
			r.Post("/{id}/approve", s.handleApproveMessage)
			r.Delete("/{id}", s.handleRejectMessage)
			r.Patch("/{id}", s.handleEditMessage)
		})

		r.Route("/templates", func(r chi.Router) {
			r.Get("/", s.handleListTemplates)
			r.Put("/{key}", s.handleSetTemplate)
		})

		r.Route("/settings", func(r chi.Router) {
			r.Get("/", s.handleListSettings)
			r.Put("/", s.handleSetSetting)
		})
	})

	// Serve the built React app (if present) with SPA fallback.
	s.mountStatic(r)
	return r
}

func (s *Server) mountStatic(r chi.Router) {
	dist := "web/dist"
	if _, err := os.Stat(dist); err != nil {
		return
	}
	fs := http.FileServer(http.Dir(dist))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		// API/tracking routes already handled above; everything else is the SPA.
		path := filepath.Join(dist, filepath.Clean(req.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, req)
			return
		}
		http.ServeFile(w, req, filepath.Join(dist, "index.html"))
	})
}

// --- helpers ---

func (s *Server) userID(r *http.Request) int64 { return auth.UserID(r.Context()) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func idParam(r *http.Request) int64 {
	return parseInt64(chi.URLParam(r, "id"))
}

func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	if s == "" {
		return 0
	}
	return n
}

func trimLower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
