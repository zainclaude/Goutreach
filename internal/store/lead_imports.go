package store

import (
	"context"
	"time"
)

// LeadImport is one CSV upload: the file name and what happened to its rows.
type LeadImport struct {
	ID          int64     `json:"id"`
	Filename    string    `json:"filename"`
	Imported    int       `json:"imported"`
	Updated     int       `json:"updated"`
	Skipped     int       `json:"skipped"`
	Blacklisted int       `json:"blacklisted"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateLeadImport records an upload before its rows are processed, so leads
// can reference it while they're inserted. Counts are filled in afterwards.
func (s *Store) CreateLeadImport(ctx context.Context, userID int64, filename string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO lead_imports (user_id, filename) VALUES ($1,$2) RETURNING id`,
		userID, filename).Scan(&id)
	return id, err
}

// SetLeadImportCounts stores the final row tallies for an upload.
func (s *Store) SetLeadImportCounts(ctx context.Context, id int64, imported, updated, skipped, blacklisted int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE lead_imports SET imported=$2, updated=$3, skipped=$4, blacklisted=$5 WHERE id=$1`,
		id, imported, updated, skipped, blacklisted)
	return err
}

// ListLeadImports returns the user's upload history, newest first.
func (s *Store) ListLeadImports(ctx context.Context, userID int64) ([]LeadImport, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, filename, imported, updated, skipped, blacklisted, created_at
		 FROM lead_imports WHERE user_id=$1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeadImport
	for rows.Next() {
		var li LeadImport
		if err := rows.Scan(&li.ID, &li.Filename, &li.Imported, &li.Updated,
			&li.Skipped, &li.Blacklisted, &li.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, li)
	}
	return out, rows.Err()
}
