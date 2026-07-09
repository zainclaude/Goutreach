package api

import (
	"net/http"

	"github.com/zainclaude/goutreach/internal/fastmoss"
	"github.com/zainclaude/goutreach/internal/research"
)

// handleFastmossPing verifies the stored FastMoss client_secret with a live
// call and returns the API's exact response, so key problems (quota, invalid
// token) are diagnosable from Settings instead of the server logs.
func (s *Server) handleFastmossPing(w http.ResponseWriter, r *http.Request) {
	enc, ok, _ := s.st.GetSetting(r.Context(), s.userID(r), research.KeyFastmossClientSecret)
	if !ok || enc == "" {
		writeErr(w, http.StatusBadRequest, "no FastMoss client_secret saved")
		return
	}
	secret, err := s.cipher.Decrypt(enc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not decrypt stored key: "+err.Error())
		return
	}
	client := fastmoss.New(secret, "")
	// ?brand=Acme runs a real shop lookup (the same one email research uses),
	// so index coverage is testable without spending an AI generation.
	if brand := r.URL.Query().Get("brand"); brand != "" {
		m, err := client.BrandMetrics(r.Context(), brand)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "FastMoss error: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "found": m.Found, "shop_name": m.ShopName,
			"revenue_usd": m.RevenueUSD, "creators": m.Creators, "videos": m.Videos,
		})
		return
	}
	if err := client.VerifyKey(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, "FastMoss rejected the key: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
