package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// BrandEmail is a cached, brand-level email (per domain + step). The name fields
// in subject/body are tokenized ({{first_name}} etc.) so the email can be reused
// for any contact at the same company by re-personalizing.
type BrandEmail struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	Domain        string    `json:"domain"`
	StepIndex     int       `json:"step_index"`
	BrandName     string    `json:"brand_name"`
	TemplateUsed  string    `json:"template_used"`
	Subject       string    `json:"subject"`
	Body          string    `json:"body"`
	ResearchNotes string    `json:"research_notes"`
	CreatedAt     time.Time `json:"created_at"`
}

// GetBrandEmail returns the cached email for a domain+step, if one exists.
func (s *Store) GetBrandEmail(ctx context.Context, userID int64, domain string, step int) (BrandEmail, bool, error) {
	var b BrandEmail
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, domain, step_index, brand_name, template_used,
		        subject, body, research_notes, created_at
		 FROM brand_emails WHERE user_id=$1 AND domain=$2 AND step_index=$3`,
		userID, domain, step).Scan(&b.ID, &b.UserID, &b.Domain, &b.StepIndex,
		&b.BrandName, &b.TemplateUsed, &b.Subject, &b.Body, &b.ResearchNotes, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BrandEmail{}, false, nil
	}
	if err != nil {
		return BrandEmail{}, false, err
	}
	return b, true, nil
}

// SaveBrandEmail stores a brand email. First write wins (reuse forever); an
// existing entry for the same domain+step is left untouched.
func (s *Store) SaveBrandEmail(ctx context.Context, b BrandEmail) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO brand_emails
		 (user_id, domain, step_index, brand_name, template_used, subject, body, research_notes)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (user_id, domain, step_index) DO NOTHING`,
		b.UserID, b.Domain, b.StepIndex, b.BrandName, b.TemplateUsed, b.Subject, b.Body, b.ResearchNotes)
	return err
}
