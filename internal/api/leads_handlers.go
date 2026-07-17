package api

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/zainclaude/goutreach/internal/store"
)

func (s *Server) handleListLeads(w http.ResponseWriter, r *http.Request) {
	leads, err := s.st.ListLeads(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, leads)
}

type leadReq struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company"`
	Title     string `json:"title"`
}

func (s *Server) handleCreateLead(w http.ResponseWriter, r *http.Request) {
	var req leadReq
	if err := readJSON(r, &req); err != nil || req.Email == "" {
		writeErr(w, http.StatusBadRequest, "email required")
		return
	}
	if bl, _ := s.st.BlacklistedDomains(r.Context(), s.userID(r)); store.IsBlacklisted(bl, req.Email) {
		writeErr(w, http.StatusBadRequest, "domain is blacklisted: "+store.NormalizeDomain(req.Email))
		return
	}
	l, _, err := s.st.UpsertLead(r.Context(), store.Lead{
		UserID:    s.userID(r),
		Email:     trimLower(req.Email),
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Company:   req.Company,
		Title:     req.Title,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// handleImportLeads accepts a CSV (multipart "file" field or raw body). The
// header row is matched case-insensitively to email/first_name/last_name/
// company/title; any other columns are stored in custom_fields. Accepts one or
// more files in a single multipart request; each file gets its own history
// entry so leads remember which upload they came from.
func (s *Server) handleImportLeads(w http.ResponseWriter, r *http.Request) {
	userID := s.userID(r)
	files := csvFiles(r)
	if len(files) == 0 {
		writeErr(w, http.StatusBadRequest, "no CSV provided")
		return
	}
	blacklist, _ := s.st.BlacklistedDomains(r.Context(), userID)

	type fileResult struct {
		Filename    string   `json:"filename"`
		Imported    int      `json:"imported"`
		Updated     int      `json:"updated"`
		Skipped     int      `json:"skipped"`
		Blacklisted int      `json:"blacklisted"`
		Error       string   `json:"error,omitempty"`
		BlockedList []string `json:"-"`
	}
	results := []fileResult{}
	totImported, totUpdated, totSkipped := 0, 0, 0
	var allBlacklisted []string

	for _, f := range files {
		res := fileResult{Filename: f.name}
		reader := csv.NewReader(f.body)
		reader.FieldsPerRecord = -1
		rows, err := reader.ReadAll()
		if err != nil || len(rows) < 1 {
			res.Error = "could not parse CSV"
			results = append(results, res)
			continue
		}
		importID, err := s.st.CreateLeadImport(r.Context(), userID, f.name)
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		imported, updated, skipped, blocked := s.importCSVRows(r, userID, importID, rows, blacklist)
		_ = s.st.SetLeadImportCounts(r.Context(), importID, imported, updated, skipped, len(blocked))
		res.Imported, res.Updated, res.Skipped, res.Blacklisted = imported, updated, skipped, len(blocked)
		results = append(results, res)
		totImported += imported
		totUpdated += updated
		totSkipped += skipped
		allBlacklisted = append(allBlacklisted, blocked...)
	}

	// Verify newly imported addresses in the background (no-op if not configured).
	if totImported > 0 {
		s.verifyUnverifiedAsync(userID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files":    results,
		"imported": totImported, "updated": totUpdated, "skipped": totSkipped,
		"blacklisted": len(allBlacklisted), "blacklisted_emails": allBlacklisted,
	})
}

// importCSVRows runs the rows of one CSV through the lead upserter and returns
// the tallies plus the blacklisted addresses it refused.
func (s *Server) importCSVRows(r *http.Request, userID, importID int64, rows [][]string, blacklist map[string]bool) (imported, updated, skipped int, blacklisted []string) {
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[normalizeHeader(h)] = i
	}
	col := func(row []string, name string) string {
		if i, ok := idx[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	known := map[string]bool{"email": true, "first_name": true, "last_name": true, "company": true, "title": true}

	for _, row := range rows[1:] {
		email := col(row, "email")
		if email == "" {
			skipped++
			continue
		}
		if store.IsBlacklisted(blacklist, email) {
			blacklisted = append(blacklisted, strings.ToLower(email))
			continue
		}
		custom := map[string]string{}
		for h, i := range idx {
			if !known[h] && i < len(row) && strings.TrimSpace(row[i]) != "" {
				custom[h] = strings.TrimSpace(row[i])
			}
		}
		cf, _ := json.Marshal(custom)
		_, inserted, err := s.st.UpsertLead(r.Context(), store.Lead{
			UserID:       userID,
			Email:        strings.ToLower(email),
			FirstName:    col(row, "first_name"),
			LastName:     col(row, "last_name"),
			Company:      col(row, "company"),
			Title:        col(row, "title"),
			CustomFields: cf,
			ImportID:     &importID,
		})
		if err != nil {
			skipped++
			continue
		}
		if inserted {
			imported++
		} else {
			updated++
		}
	}
	return imported, updated, skipped, blacklisted
}

// handleListLeadImports returns the CSV upload history (newest first).
func (s *Server) handleListLeadImports(w http.ResponseWriter, r *http.Request) {
	imports, err := s.st.ListLeadImports(r.Context(), s.userID(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if imports == nil {
		imports = []store.LeadImport{}
	}
	writeJSON(w, http.StatusOK, imports)
}

func (s *Server) handleDeleteLead(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteLead(r.Context(), s.userID(r), idParam(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// namedCSV is one uploaded file: its client-side filename and content.
type namedCSV struct {
	name string
	body io.Reader
}

// csvFiles returns every CSV in the request. Multipart uploads may carry
// several files under the "file" field; a raw body counts as one unnamed file.
func csvFiles(r *http.Request) []namedCSV {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			return nil
		}
		var out []namedCSV
		if r.MultipartForm != nil {
			for _, fh := range r.MultipartForm.File["file"] {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				out = append(out, namedCSV{name: fh.Filename, body: f})
			}
		}
		return out
	}
	return []namedCSV{{name: "pasted.csv", body: r.Body}}
}

func csvReader(r *http.Request) io.Reader {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil
		}
		return file
	}
	return r.Body
}

func normalizeHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.ReplaceAll(h, " ", "_")
	switch h {
	case "first", "firstname", "first_name", "fname":
		return "first_name"
	case "last", "lastname", "last_name", "lname":
		return "last_name"
	case "company", "company_name", "brand", "organization":
		return "company"
	case "title", "job_title", "role", "position":
		return "title"
	case "email", "email_address", "e-mail":
		return "email"
	}
	return h
}
