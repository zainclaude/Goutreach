package store

import "context"

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
