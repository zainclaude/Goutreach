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
	Contacted    bool            `json:"contacted"`           // has ever had an email actually sent
	Verification string          `json:"verification_status"` // unknown|valid|invalid|risky|catch_all
	VerifiedAt   *time.Time      `json:"verified_at"`
	ImportID     *int64          `json:"import_id,omitempty"` // upload the lead first arrived in
	SourceFile   string          `json:"source_file"`         // filename of that upload ("" if added manually)
}

// contactedExpr is true when a lead has a message that was actually sent (in any
// campaign). Reused by lead/enrollment list queries to show a contacted badge.
const contactedExpr = `EXISTS(SELECT 1 FROM campaign_leads cl JOIN messages m ON m.campaign_lead_id=cl.id
	WHERE cl.lead_id = leads.id AND m.status IN ('sent','replied','bounced'))`

const leadCols = `id, user_id, email, first_name, last_name, company, title, custom_fields, status, created_at, verification_status, verified_at`

func scanLead(row interface{ Scan(...any) error }) (Lead, error) {
	var l Lead
	err := row.Scan(&l.ID, &l.UserID, &l.Email, &l.FirstName, &l.LastName, &l.Company,
		&l.Title, &l.CustomFields, &l.Status, &l.CreatedAt, &l.Verification, &l.VerifiedAt)
	return l, err
}

// SetLeadVerification records a verification result for a lead.
func (s *Store) SetLeadVerification(ctx context.Context, id int64, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE leads SET verification_status=$2, verified_at=now() WHERE id=$1`, id, status)
	return err
}

// ListUnverifiedLeads returns leads that have not been verified yet (status
// 'unknown'), for the user, up to limit.
func (s *Store) ListUnverifiedLeads(ctx context.Context, userID int64, limit int) ([]Lead, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+leadCols+` FROM leads WHERE user_id=$1 AND verification_status='unknown'
		 ORDER BY id LIMIT $2`, userID, limit)
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

// UpsertLead inserts or updates a lead by (user_id, email). Returns whether it was newly created.
func (s *Store) UpsertLead(ctx context.Context, l Lead) (Lead, bool, error) {
	if len(l.CustomFields) == 0 {
		l.CustomFields = json.RawMessage(`{}`)
	}
	// import_id keeps the FIRST file the lead arrived in — re-importing an
	// existing lead updates its fields but not its origin.
	row := s.pool.QueryRow(ctx,
		`INSERT INTO leads (user_id, email, first_name, last_name, company, title, custom_fields, import_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (user_id, email) DO UPDATE SET
		   first_name = EXCLUDED.first_name,
		   last_name  = EXCLUDED.last_name,
		   company    = EXCLUDED.company,
		   title      = EXCLUDED.title,
		   custom_fields = EXCLUDED.custom_fields,
		   import_id  = COALESCE(leads.import_id, EXCLUDED.import_id)
		 RETURNING `+leadCols+`, (xmax = 0) AS inserted`,
		l.UserID, l.Email, l.FirstName, l.LastName, l.Company, l.Title, l.CustomFields, l.ImportID)
	var out Lead
	var inserted bool
	err := row.Scan(&out.ID, &out.UserID, &out.Email, &out.FirstName, &out.LastName,
		&out.Company, &out.Title, &out.CustomFields, &out.Status, &out.CreatedAt,
		&out.Verification, &out.VerifiedAt, &inserted)
	return out, inserted, err
}

// ListLeads returns leads for a user, with the filename of the upload each
// lead first arrived in ("" for manually added leads).
func (s *Store) ListLeads(ctx context.Context, userID int64) ([]Lead, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+leadCols+`, `+contactedExpr+` AS contacted,
		        COALESCE((SELECT li.filename FROM lead_imports li WHERE li.id = leads.import_id), '')
		 FROM leads WHERE user_id=$1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lead
	for rows.Next() {
		var l Lead
		if err := rows.Scan(&l.ID, &l.UserID, &l.Email, &l.FirstName, &l.LastName, &l.Company,
			&l.Title, &l.CustomFields, &l.Status, &l.CreatedAt, &l.Verification, &l.VerifiedAt,
			&l.Contacted, &l.SourceFile); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListUnemailedLeads returns leads that have never actually been *sent* an email
// (no message in a sent/replied/bounced state). Leads that were only previewed —
// enrolled with a draft that was never sent (queued/generating/generated/failed) —
// still count as unemailed, so "enroll all unemailed" picks them up.
func (s *Store) ListUnemailedLeads(ctx context.Context, userID int64) ([]Lead, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+leadCols+` FROM leads
		 WHERE user_id=$1 AND id NOT IN (
		   SELECT cl.lead_id FROM campaign_leads cl
		   JOIN messages m ON m.campaign_lead_id = cl.id
		   WHERE m.status IN ('sent','replied','bounced')
		 )
		 ORDER BY id DESC`, userID)
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
