// Package store contains the data models and all Postgres access for Goutreach.
package store

import "github.com/jackc/pgx/v5/pgxpool"

// Store is the data-access layer over a pgx pool.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by the given pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the underlying pool for callers that need ad-hoc access.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }
