package store

import "context"

// ListAllActiveAccounts returns every active account across users (single-tenant
// MVP), used by the IMAP reply poller.
func (s *Store) ListAllActiveAccounts(ctx context.Context) ([]EmailAccount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+accountCols+` FROM email_accounts WHERE status='active' ORDER BY id`)
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
