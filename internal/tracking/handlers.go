// Package tracking serves open/click endpoints and polls IMAP for replies/bounces.
package tracking

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"github.com/zainclaude/goutreach/internal/store"
)

// transparent 1x1 GIF
var pixelGIF, _ = base64.StdEncoding.DecodeString(
	"R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")

// Handlers serves the public open/click tracking endpoints.
type Handlers struct {
	st *store.Store
}

// NewHandlers builds tracking HTTP handlers.
func NewHandlers(st *store.Store) *Handlers { return &Handlers{st: st} }

// Open records an open event (deduplicated) and returns a 1x1 pixel.
// Path: /t/open/{id}.png
func (h *Handlers) Open(w http.ResponseWriter, r *http.Request) {
	id := parseID(strings.TrimSuffix(pathTail(r.URL.Path), ".png"))
	if id > 0 {
		if seen, _ := h.st.HasEvent(r.Context(), id, "open"); !seen {
			_ = h.st.CreateEvent(r.Context(), id, "open", map[string]any{
				"ua": r.UserAgent(),
			})
		}
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pixelGIF)
}

// Click records a click and redirects to the target URL.
// Path: /t/click/{id}?u=<encoded url>
func (h *Handlers) Click(w http.ResponseWriter, r *http.Request) {
	id := parseID(pathTail(r.URL.Path))
	target := r.URL.Query().Get("u")
	if id > 0 {
		_ = h.st.CreateEvent(r.Context(), id, "click", map[string]any{"url": target})
	}
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func pathTail(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return p
	}
	return p[i+1:]
}

func parseID(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
