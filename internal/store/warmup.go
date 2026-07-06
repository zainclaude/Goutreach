package store

import (
	"context"
	"time"
)

// RecordWarmupSent records an outbound warmup message.
func (s *Store) RecordWarmupSent(ctx context.Context, fromAccount, toAccount int64, subject, messageID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO warmup_messages (from_account_id, to_account_id, subject, message_id)
		 VALUES ($1,$2,$3,$4)`, fromAccount, toAccount, subject, messageID)
	return err
}

// CountWarmupSentToday returns how many warmup messages an account sent since midnight Eastern (America/New_York).
func (s *Store) CountWarmupSentToday(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM warmup_messages
		 WHERE from_account_id=$1 AND created_at >= date_trunc('day', now() AT TIME ZONE 'America/New_York') AT TIME ZONE 'America/New_York'`,
		accountID).Scan(&n)
	return n, err
}

// FirstWarmupSentAt returns when an account sent its first warmup message. The
// warmup ramp is anchored here (not account creation) so every inbox eases in
// from 2/day on its first sending day regardless of how long it sat idle. The
// bool is false if the account has never sent a warmup message.
func (s *Store) FirstWarmupSentAt(ctx context.Context, accountID int64) (time.Time, bool, error) {
	var t *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT min(created_at) FROM warmup_messages WHERE from_account_id=$1`,
		accountID).Scan(&t)
	if err != nil || t == nil {
		return time.Time{}, false, err
	}
	return *t, true, nil
}

// FindWarmupByMessageID returns whether an inbound message-id corresponds to one
// of our warmup sends (so the IMAP poller can mark it received, not treat it as a reply).
func (s *Store) FindWarmupByMessageID(ctx context.Context, messageID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM warmup_messages WHERE message_id=$1)`, messageID).Scan(&exists)
	return exists, err
}

// SetWarmupStatusByMessageID records where a warmup message landed
// (received = inbox, spam = junk folder). Keyed by message_id (the sender's row).
func (s *Store) SetWarmupStatusByMessageID(ctx context.Context, messageID, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE warmup_messages SET status=$2 WHERE message_id=$1`, messageID, status)
	return err
}

// WarmupStat is per-account warmup placement over the last 30 days.
type WarmupStat struct {
	AccountID int64   `json:"account_id"`
	Email     string  `json:"email"`
	Enabled   bool    `json:"warmup_enabled"`
	Sent      int     `json:"sent"`
	Inbox     int     `json:"inbox"`
	Spam      int     `json:"spam"`
	Pending   int     `json:"pending"`    // sent but placement not yet detected
	InboxRate float64 `json:"inbox_rate"` // inbox / (inbox+spam)
	SentToday int     `json:"sent_today"`
}

// WarmupStats returns per-account warmup placement (by sending account) for the
// user, over the last 30 days.
func (s *Store) WarmupStats(ctx context.Context, userID int64) ([]WarmupStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.email, a.warmup_enabled,
		  count(w.id)                                              AS sent,
		  count(w.id) FILTER (WHERE w.status='received')          AS inbox,
		  count(w.id) FILTER (WHERE w.status='spam')              AS spam,
		  count(w.id) FILTER (WHERE w.created_at >= date_trunc('day', now() AT TIME ZONE 'America/New_York') AT TIME ZONE 'America/New_York') AS sent_today
		FROM email_accounts a
		LEFT JOIN warmup_messages w
		  ON w.from_account_id = a.id AND w.created_at >= now() - interval '30 days'
		WHERE a.user_id=$1
		GROUP BY a.id, a.email, a.warmup_enabled
		ORDER BY a.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WarmupStat
	for rows.Next() {
		var st WarmupStat
		if err := rows.Scan(&st.AccountID, &st.Email, &st.Enabled, &st.Sent, &st.Inbox, &st.Spam, &st.SentToday); err != nil {
			return nil, err
		}
		st.Pending = st.Sent - st.Inbox - st.Spam
		if st.Pending < 0 {
			st.Pending = 0
		}
		if placed := st.Inbox + st.Spam; placed > 0 {
			st.InboxRate = float64(st.Inbox) / float64(placed)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
