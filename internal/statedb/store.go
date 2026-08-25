// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package statedb is the daemon product store (🎯T548): one SQLite file
// under StateDir holds coalesced transcript rows and the fleet agent
// tree. JSONL journals remain import-once history and a vanilla-inspect
// compatibility arm — they are not the mux source of truth.
//
// Durability: PRAGMA synchronous=FULL so each committed write fsyncs,
// matching chatlog.Append. WAL + a single connection matches the
// modernc.org/sqlite pattern in internal/cost and internal/provider.
package statedb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo
)

// SchemaVersion is the store format. Bump only for incompatible layout.
const SchemaVersion = 1

// Store is jevonsd's product SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

const schema = `
CREATE TABLE IF NOT EXISTS schema_meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS transcript_events (
	agent TEXT NOT NULL,
	idx   INTEGER NOT NULL,
	id    TEXT NOT NULL,
	ts    TEXT NOT NULL DEFAULT '',
	typ   TEXT NOT NULL DEFAULT '',
	kind  INTEGER NOT NULL DEFAULT 0,
	body  TEXT NOT NULL,
	PRIMARY KEY (agent, idx)
);

CREATE TABLE IF NOT EXISTS agents (
	name       TEXT PRIMARY KEY,
	parent     TEXT NOT NULL DEFAULT '',
	purpose    TEXT NOT NULL DEFAULT '',
	target_id  TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	provider   TEXT NOT NULL DEFAULT '',
	model      TEXT NOT NULL DEFAULT '',
	workdir    TEXT NOT NULL DEFAULT '',
	status     TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS import_watermark (
	agent      TEXT PRIMARY KEY,
	jsonl_path TEXT NOT NULL,
	jsonl_size INTEGER NOT NULL,
	imported_n INTEGER NOT NULL,
	imported_at TEXT NOT NULL
);
`

// DefaultPath returns StateDir/jevons.db.
func DefaultPath(stateDir string) string {
	return filepath.Join(stateDir, "jevons.db")
}

// Open opens (or creates) the product database at path.
// Parent directories are created. Use ":memory:" for hermetic tests.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("statedb: mkdir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("statedb: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=FULL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("statedb: %s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("statedb: schema: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.ensureSchemaVersion(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureSchemaVersion() error {
	var v string
	err := s.db.QueryRow(`SELECT value FROM schema_meta WHERE key = 'schema_version'`).Scan(&v)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec(
			`INSERT INTO schema_meta(key, value) VALUES ('schema_version', ?)`,
			fmt.Sprintf("%d", SchemaVersion),
		)
		return err
	}
	if err != nil {
		return fmt.Errorf("statedb: schema_version: %w", err)
	}
	return nil
}

// Path returns the database file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
