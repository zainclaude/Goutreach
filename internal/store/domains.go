package store

import (
	"context"
	"time"
)

// Domain is a sending domain managed by PipelineBuilder. It is registered (or
// imported) via a registrar, has cold-email DNS applied, then verified with the
// mailbox provider so inboxes can be provisioned on it.
type Domain struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	Domain     string    `json:"domain"`
	Registrar  string    `json:"registrar"`
	Status     string    `json:"status"` // pending|purchased|dns_ready|verified|error
	DNSApplied bool      `json:"dns_applied"`
	DKIMRecord string    `json:"dkim_record"`
	LastError  string    `json:"last_error"`
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
}

const domainCols = `id, user_id, domain, registrar, status, dns_applied,
	dkim_record, last_error, notes, created_at`

func scanDomain(row interface{ Scan(...any) error }) (Domain, error) {
	var d Domain
	err := row.Scan(&d.ID, &d.UserID, &d.Domain, &d.Registrar, &d.Status,
		&d.DNSApplied, &d.DKIMRecord, &d.LastError, &d.Notes, &d.CreatedAt)
	return d, err
}

// CreateDomain inserts a domain (idempotent on user_id+domain).
func (s *Store) CreateDomain(ctx context.Context, d Domain) (Domain, error) {
	if d.Registrar == "" {
		d.Registrar = "porkbun"
	}
	if d.Status == "" {
		d.Status = "pending"
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO domains (user_id, domain, registrar, status, dns_applied, dkim_record, last_error, notes)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (user_id, domain) DO UPDATE SET
		   registrar = EXCLUDED.registrar,
		   status = EXCLUDED.status,
		   last_error = EXCLUDED.last_error
		 RETURNING `+domainCols,
		d.UserID, d.Domain, d.Registrar, d.Status, d.DNSApplied, d.DKIMRecord, d.LastError, d.Notes)
	return scanDomain(row)
}

// ListDomains returns all domains for a user.
func (s *Store) ListDomains(ctx context.Context, userID int64) ([]Domain, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+domainCols+` FROM domains WHERE user_id=$1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDomain returns a single domain scoped to a user.
func (s *Store) GetDomain(ctx context.Context, userID, id int64) (Domain, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+domainCols+` FROM domains WHERE id=$1 AND user_id=$2`, id, userID)
	return scanDomain(row)
}

// SetDomainStatus updates status + last_error.
func (s *Store) SetDomainStatus(ctx context.Context, userID, id int64, status, lastErr string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET status=$3, last_error=$4 WHERE id=$1 AND user_id=$2`,
		id, userID, status, lastErr)
	return err
}

// SetDomainDNSApplied marks whether the cold-email DNS records were written.
func (s *Store) SetDomainDNSApplied(ctx context.Context, userID, id int64, applied bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET dns_applied=$3 WHERE id=$1 AND user_id=$2`, id, userID, applied)
	return err
}

// SetDomainDKIM stores the DKIM TXT value provided by the mailbox provider.
func (s *Store) SetDomainDKIM(ctx context.Context, userID, id int64, dkim string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE domains SET dkim_record=$3 WHERE id=$1 AND user_id=$2`, id, userID, dkim)
	return err
}

// DeleteDomain removes a domain (does not affect the registration at Porkbun).
func (s *Store) DeleteDomain(ctx context.Context, userID, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM domains WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}
