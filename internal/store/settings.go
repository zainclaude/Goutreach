package store

import (
	"context"
	"strings"
)

// NormalizeDomain lowercases and reduces an email or URL to a bare domain:
// "Mary@Sub.MaryRuths.com" -> "sub.maryruths.com", "https://www.x.com/p" -> "x.com".
func NormalizeDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:] // email local-part -> domain
	}
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "www.")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// BlacklistedDomains returns the set of domains the user never wants to email,
// parsed from the "blacklist_domains" setting (comma/space/newline separated).
func (s *Store) BlacklistedDomains(ctx context.Context, userID int64) (map[string]bool, error) {
	raw, ok, err := s.GetSetting(ctx, userID, "blacklist_domains")
	set := map[string]bool{}
	if err != nil || !ok {
		return set, err
	}
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == ';' || r == '\t'
	}) {
		if d := NormalizeDomain(f); d != "" {
			set[d] = true
		}
	}
	return set, nil
}

// IsBlacklisted reports whether an email's domain is in the blacklist set.
func IsBlacklisted(set map[string]bool, email string) bool {
	return len(set) > 0 && set[NormalizeDomain(email)]
}

// Setting is a key/value configuration entry, optionally secret.
type Setting struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

// SetSetting upserts a setting. Secret values should already be encrypted by the caller.
func (s *Store) SetSetting(ctx context.Context, userID int64, key, value string, isSecret bool) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO settings (user_id, key, value, is_secret, updated_at)
		 VALUES ($1,$2,$3,$4, now())
		 ON CONFLICT (user_id, key) DO UPDATE SET value=EXCLUDED.value, is_secret=EXCLUDED.is_secret, updated_at=now()`,
		userID, key, value, isSecret)
	return err
}

// GetSetting returns a single setting value (empty if missing).
func (s *Store) GetSetting(ctx context.Context, userID int64, key string) (string, bool, error) {
	var v string
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM settings WHERE user_id=$1 AND key=$2`, userID, key).Scan(&v)
	if err != nil {
		return "", false, nil
	}
	return v, true, nil
}

// ListSettingKeys returns the keys present for a user (values omitted for secrets).
func (s *Store) ListSettingKeys(ctx context.Context, userID int64) ([]Setting, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, value, is_secret FROM settings WHERE user_id=$1 ORDER BY key`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Setting
	for rows.Next() {
		var st Setting
		if err := rows.Scan(&st.Key, &st.Value, &st.IsSecret); err != nil {
			return nil, err
		}
		if st.IsSecret {
			st.Value = "" // never expose secret values
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
