// Package store is Aperture's persistence layer: a thin, dependency-light
// wrapper over SQLite (pure-Go modernc driver) with embedded migrations.
package store

import (
	"database/sql"
	"fmt"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// Store owns the database handle and exposes typed data-access methods.
type Store struct {
	db *sql.DB
	// eventSeq is the in-process source for events.seq: strictly increasing (not
	// gapless), seeded at Open from the high-water mark. Safe under the
	// single-writer pool with no per-insert SELECT.
	eventSeq atomic.Int64
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
	// Seed the event-sequence counter from the high-water mark so new events keep
	// strictly increasing across restarts (the 0022 backfill sets existing rows).
	var maxSeq sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&maxSeq); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("seed event seq: %w", err)
	}
	s.eventSeq.Store(maxSeq.Int64)
	return s, nil
}

// DB exposes the raw handle for packages that need ad-hoc queries.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }
