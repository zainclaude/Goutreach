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
// company/title; any other columns are stored in custom_fields.
func (s *Server) handleImportLeads(w http.ResponseWriter, r *http.Request) {
	body := csvReader(r)
	if body == nil {
		writeErr(w, http.StatusBadRequest, "no CSV provided")
		return
	}
	reader := csv.NewReader(body)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil || len(rows) < 1 {
		writeErr(w, http.StatusBadRequest, "could not parse CSV")
		return
	}

	header := rows[0]
	idx := map[string]int{}
	for i, h := range header {
		idx[normalizeHeader(h)] = i
	}
	col := func(row []string, name string) string {
		if i, ok := idx[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	known := map[string]bool{"email": true, "first_name": true, "last_name": true, "company": true, "title": true}

	imported, updated, skipped := 0, 0, 0
	for _, row := range rows[1:] {
		email := col(row, "email")
		if email == "" {
			skipped++
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
			UserID:       s.userID(r),
			Email:        strings.ToLower(email),
			FirstName:    col(row, "first_name"),
			LastName:     col(row, "last_name"),
			Company:      col(row, "company"),
			Title:        col(row, "title"),
			CustomFields: cf,
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
	writeJSON(w, http.StatusOK, map[string]int{"imported": imported, "updated": updated, "skipped": skipped})
}

func (s *Server) handleDeleteLead(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteLead(r.Context(), s.userID(r), idParam(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
