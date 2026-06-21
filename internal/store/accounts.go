package store

import (
	"context"
	"time"
)

// EmailAccount is a connected sending/receiving mailbox. Password fields hold
// AES-GCM ciphertext and are never serialized to JSON.
type EmailAccount struct {
	ID                 int64     `json:"id"`
	UserID             int64     `json:"user_id"`
	Email              string    `json:"email"`
	FromName           string    `json:"from_name"`
	SMTPHost           string    `json:"smtp_host"`
	SMTPPort           int       `json:"smtp_port"`
	SMTPUsername       string    `json:"smtp_username"`
	SMTPPasswordEnc    string    `json:"-"`
	IMAPHost           string    `json:"imap_host"`
	IMAPPort           int       `json:"imap_port"`
	IMAPUsername       string    `json:"imap_username"`
	IMAPPasswordEnc    string    `json:"-"`
	DailyLimit         int       `json:"daily_limit"`
	WarmupEnabled      bool      `json:"warmup_enabled"`
	WarmupTargetPerDay int       `json:"warmup_target_per_day"`
	Status             string    `json:"status"`
	LastError          string    `json:"last_error"`
	CreatedAt          time.Time `json:"created_at"`
}

const accountCols = `id, user_id, email, from_name, smtp_host, smtp_port, smtp_username,
	smtp_password_enc, imap_host, imap_port, imap_username, imap_password_enc,
	daily_limit, warmup_enabled, warmup_target_per_day, status, last_error, created_at`

func scanAccount(row interface{ Scan(...any) error }) (EmailAccount, error) {
	var a EmailAccount
	err := row.Scan(&a.ID, &a.UserID, &a.Email, &a.FromName, &a.SMTPHost, &a.SMTPPort,
		&a.SMTPUsername, &a.SMTPPasswordEnc, &a.IMAPHost, &a.IMAPPort, &a.IMAPUsername,
		&a.IMAPPasswordEnc, &a.DailyLimit, &a.WarmupEnabled, &a.WarmupTargetPerDay,
		&a.Status, &a.LastError, &a.CreatedAt)
	return a, err
}

// CreateAccount inserts a new email account.
func (s *Store) CreateAccount(ctx context.Context, a EmailAccount) (EmailAccount, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO email_accounts
		 (user_id, email, from_name, smtp_host, smtp_port, smtp_username, smtp_password_enc,
		  imap_host, imap_port, imap_username, imap_password_enc, daily_limit,
		  warmup_enabled, warmup_target_per_day)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 RETURNING `+accountCols,
		a.UserID, a.Email, a.FromName, a.SMTPHost, a.SMTPPort, a.SMTPUsername, a.SMTPPasswordEnc,
		a.IMAPHost, a.IMAPPort, a.IMAPUsername, a.IMAPPasswordEnc, a.DailyLimit,
		a.WarmupEnabled, a.WarmupTargetPerDay)
	return scanAccount(row)
}

// ListAccounts returns all accounts for a user.
func (s *Store) ListAccounts(ctx context.Context, userID int64) ([]EmailAccount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+accountCols+` FROM email_accounts WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EmailAccount
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAccount returns a single account scoped to a user.
func (s *Store) GetAccount(ctx context.Context, userID, id int64) (EmailAccount, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+accountCols+` FROM email_accounts WHERE id=$1 AND user_id=$2`, id, userID)
	return scanAccount(row)
}

// GetAccountByID returns an account without a user scope (for internal workers).
func (s *Store) GetAccountByID(ctx context.Context, id int64) (EmailAccount, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+accountCols+` FROM email_accounts WHERE id=$1`, id)
	return scanAccount(row)
}

// ListWarmupAccounts returns all accounts with warmup enabled (across users in MVP single-tenant).
func (s *Store) ListWarmupAccounts(ctx context.Context) ([]EmailAccount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+accountCols+` FROM email_accounts WHERE warmup_enabled=true AND status='active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EmailAccount
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAccount removes an account.
func (s *Store) DeleteAccount(ctx context.Context, userID, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM email_accounts WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

// SetAccountStatus updates status + last_error.
func (s *Store) SetAccountStatus(ctx context.Context, id int64, status, lastErr string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE email_accounts SET status=$2, last_error=$3 WHERE id=$1`, id, status, lastErr)
	return err
}

// UpdateAccountSettings updates the mutable knobs on an account.
func (s *Store) UpdateAccountSettings(ctx context.Context, userID, id int64, fromName string, dailyLimit int, warmupEnabled bool, warmupTarget int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE email_accounts SET from_name=$3, daily_limit=$4, warmup_enabled=$5, warmup_target_per_day=$6
		 WHERE id=$1 AND user_id=$2`,
		id, userID, fromName, dailyLimit, warmupEnabled, warmupTarget)
	return err
}

// CountSentToday returns how many messages an account has sent since midnight UTC.
func (s *Store) CountSentToday(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages
		 WHERE account_id=$1 AND status='sent' AND sent_at >= date_trunc('day', now())`,
		accountID).Scan(&n)
	return n, err
}
