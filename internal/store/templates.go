package store

import (
	"context"
	"time"
)

// EmailTemplate is one of the five research-selected templates (keys A–E).
type EmailTemplate struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TemplateKeys is the canonical ordered set of template keys.
var TemplateKeys = []string{"A", "B", "C", "D", "E"}

// TemplateDefaults gives each key a human label describing when it's selected.
var TemplateDefaults = map[string]string{
	"A": "Brand is on TikTok Shop",
	"B": "Brand is on Amazon",
	"C": "Brand is running Meta ads",
	"D": "Brand has big retail presence",
	"E": "Generic (no signals found)",
}

// UpsertTemplate inserts or updates a template by (user, key).
func (s *Store) UpsertTemplate(ctx context.Context, userID int64, t EmailTemplate) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO email_templates (user_id, key, name, subject, body, updated_at)
		 VALUES ($1,$2,$3,$4,$5, now())
		 ON CONFLICT (user_id, key) DO UPDATE SET
		   name=EXCLUDED.name, subject=EXCLUDED.subject, body=EXCLUDED.body, updated_at=now()`,
		userID, t.Key, t.Name, t.Subject, t.Body)
	return err
}

// ListTemplates returns all five templates for a user, filling in any missing
// keys with empty placeholders so the UI always shows A–E.
func (s *Store) ListTemplates(ctx context.Context, userID int64) ([]EmailTemplate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, name, subject, body, updated_at FROM email_templates WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string]EmailTemplate{}
	for rows.Next() {
		var t EmailTemplate
		if err := rows.Scan(&t.Key, &t.Name, &t.Subject, &t.Body, &t.UpdatedAt); err != nil {
			return nil, err
		}
		byKey[t.Key] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]EmailTemplate, 0, len(TemplateKeys))
	for _, k := range TemplateKeys {
		if t, ok := byKey[k]; ok {
			out = append(out, t)
		} else {
			out = append(out, EmailTemplate{Key: k, Name: TemplateDefaults[k]})
		}
	}
	return out, nil
}
