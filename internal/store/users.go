package store

import (
	"context"
	"time"
)

// User is a dashboard login.
type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateUser inserts a new user and returns it.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, password_hash, created_at`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

// GetUserByEmail looks up a user by email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

// FirstUserID returns the id of the earliest-created user (single-tenant MVP).
func (s *Store) FirstUserID(ctx context.Context) (int64, bool) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// CountUsers returns the number of registered users (used to gate signup in MVP).
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}
