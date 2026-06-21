package store

import (
	"context"
	"encoding/json"
	"time"
)

// Lead is a prospect.
type Lead struct {
	ID           int64           `json:"id"`
	UserID       int64           `json:"user_id"`
	Email        string          `json:"email"`
	FirstName    string          `json:"first_name"`
	LastName     string          `json:"last_name"`
	Company      string          `json:"company"`
	Title        string          `json:"title"`
	CustomFields json.RawMessage `json:"custom_fields"`
	Status       string          `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
}

const leadCols = `id, user_id, email, first_name, last_name, company, title, custom_fields, status, created_at`

func scanLead(row interface{ Scan(...any) error }) (Lead, error) {
	var l Lead
	err := row.Scan(&l.ID, &l.UserID, &l.Email, &l.FirstName, &l.LastName, &l.Company,
		&l.Title, &l.CustomFields, &l.Status, &l.CreatedAt)
	return l, err
}

// UpsertLead inserts or updates a lead by (user_id, email). Returns whether it was newly created.
func (s *Store) UpsertLead(ctx context.Context, l Lead) (Lead, bool, error) {
	if len(l.CustomFields) == 0 {
		l.CustomFields = json.RawMessage(`{}`)
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO leads (user_id, email, first_name, last_name, company, title, custom_fields)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (user_id, email) DO UPDATE SET
		   first_name = EXCLUDED.first_name,
		   last_name  = EXCLUDED.last_name,
		   company    = EXCLUDED.company,
		   title      = EXCLUDED.title,
		   custom_fields = EXCLUDED.custom_fields
		 RETURNING `+leadCols+`, (xmax = 0) AS inserted`,
		l.UserID, l.Email, l.FirstName, l.LastName, l.Company, l.Title, l.CustomFields)
	var out Lead
	var inserted bool
	err := row.Scan(&out.ID, &out.UserID, &out.Email, &out.FirstName, &out.LastName,
		&out.Company, &out.Title, &out.CustomFields, &out.Status, &out.CreatedAt, &inserted)
	return out, inserted, err
}

// ListLeads returns leads for a user.
func (s *Store) ListLeads(ctx context.Context, userID int64) ([]Lead, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+leadCols+` FROM leads WHERE user_id=$1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lead
	for rows.Next() {
		l, err := scanLead(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetLead returns one lead (no user scope, for internal workers).
func (s *Store) GetLead(ctx context.Context, id int64) (Lead, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+leadCols+` FROM leads WHERE id=$1`, id)
	return scanLead(row)
}

// DeleteLead removes a lead.
func (s *Store) DeleteLead(ctx context.Context, userID, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM leads WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}
