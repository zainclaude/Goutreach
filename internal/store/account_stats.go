package store

import "context"

// AccountStat holds engagement metrics for one inbox.
type AccountStat struct {
	AccountID       int64   `json:"account_id"`
	Email           string  `json:"email"`
	SentToday       int     `json:"sent_today"`
	SentLifetime    int     `json:"sent_lifetime"`
	RepliesToday    int     `json:"replies_today"`
	RepliesLifetime int     `json:"replies_lifetime"`
	ReplyRate       float64 `json:"reply_rate"`
}

// AccountStats returns per-account metrics for a user.
func (s *Store) AccountStats(ctx context.Context, userID int64) ([]AccountStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.email,
		 (SELECT count(*) FROM messages m
		    WHERE m.account_id=a.id AND m.status IN ('sent','replied','bounced')
		      AND m.sent_at >= date_trunc('day', now() AT TIME ZONE 'America/New_York') AT TIME ZONE 'America/New_York') AS sent_today,
		 (SELECT count(*) FROM messages m
		    WHERE m.account_id=a.id AND m.status IN ('sent','replied','bounced')) AS sent_lifetime,
		 (SELECT count(DISTINCT e.message_id) FROM events e
		    JOIN messages m ON m.id=e.message_id
		    WHERE m.account_id=a.id AND e.type='reply'
		      AND e.created_at >= date_trunc('day', now() AT TIME ZONE 'America/New_York') AT TIME ZONE 'America/New_York') AS replies_today,
		 (SELECT count(DISTINCT e.message_id) FROM events e
		    JOIN messages m ON m.id=e.message_id
		    WHERE m.account_id=a.id AND e.type='reply') AS replies_lifetime
		FROM email_accounts a
		WHERE a.user_id=$1
		ORDER BY a.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountStat
	for rows.Next() {
		var st AccountStat
		if err := rows.Scan(&st.AccountID, &st.Email, &st.SentToday, &st.SentLifetime,
			&st.RepliesToday, &st.RepliesLifetime); err != nil {
			return nil, err
		}
		if st.SentLifetime > 0 {
			st.ReplyRate = float64(st.RepliesLifetime) / float64(st.SentLifetime)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
