package store

import "context"

// RecordWarmupSent records an outbound warmup message.
func (s *Store) RecordWarmupSent(ctx context.Context, fromAccount, toAccount int64, subject, messageID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO warmup_messages (from_account_id, to_account_id, subject, message_id)
		 VALUES ($1,$2,$3,$4)`, fromAccount, toAccount, subject, messageID)
	return err
}

// CountWarmupSentToday returns how many warmup messages an account sent since midnight UTC.
func (s *Store) CountWarmupSentToday(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM warmup_messages
		 WHERE from_account_id=$1 AND created_at >= date_trunc('day', now())`,
		accountID).Scan(&n)
	return n, err
}

// FindWarmupByMessageID returns whether an inbound message-id corresponds to one
// of our warmup sends (so the IMAP poller can mark it received, not treat it as a reply).
func (s *Store) FindWarmupByMessageID(ctx context.Context, messageID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM warmup_messages WHERE message_id=$1)`, messageID).Scan(&exists)
	return exists, err
}
