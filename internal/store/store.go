// Package store is Aperture's persistence layer: a thin, dependency-light
// wrapper over SQLite (pure-Go modernc driver) with embedded migrations.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store owns the database handle and exposes typed data-access methods.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path, applies pragmas for
// reliable concurrent access, and runs any pending migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single writer at a time; readers via WAL. Keep the pool small so the
	// busy_timeout does the waiting rather than surfacing SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=10000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// DB exposes the raw handle for packages that need ad-hoc queries.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }
