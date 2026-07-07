// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo
)

// Store persists usage events and tail offsets in SQLite. It is safe for
// concurrent use by the collector (writes) and monitor/API (reads).
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS usage_events (
	id                  INTEGER PRIMARY KEY,
	ts                  TEXT NOT NULL,
	session_id          TEXT NOT NULL,
	worker              TEXT NOT NULL DEFAULT '',
	model               TEXT NOT NULL,
	input_tokens        INTEGER NOT NULL,
	output_tokens       INTEGER NOT NULL,
	cache_create_tokens INTEGER NOT NULL,
	cache_read_tokens   INTEGER NOT NULL,
	cost_usd            REAL NOT NULL,
	request_id          TEXT UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_usage_ts ON usage_events(ts);
CREATE INDEX IF NOT EXISTS idx_usage_session ON usage_events(session_id, ts);

CREATE TABLE IF NOT EXISTS tail_state (
	path   TEXT PRIMARY KEY,
	offset INTEGER NOT NULL
);
`

// OpenStore opens (creating if needed) the usage database at path.
// Use ":memory:" for tests.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open usage db: %w", err)
	}
	// The collector writes while the monitor and API read; WAL plus a
	// busy timeout keeps that contention invisible. A single connection
	// sidesteps modernc's table-lock errors under concurrent writers.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create usage schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// InsertEvents records a batch of events. Duplicates (same request_id)
// are ignored, which makes re-ingesting a re-read or truncated-then-
// replayed transcript idempotent. Returns the number actually inserted.
func (s *Store) InsertEvents(events []*Event) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO usage_events
		(ts, session_id, worker, model, input_tokens, output_tokens,
		 cache_create_tokens, cache_read_tokens, cost_usd, request_id)
		VALUES (?,?,?,?,?,?,?,?,?,NULLIF(?,''))`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, e := range events {
		res, err := stmt.Exec(
			e.Timestamp.UTC().Format(time.RFC3339Nano),
			e.SessionID, e.Worker, e.Model,
			e.Usage.Input, e.Usage.Output, e.Usage.CacheCreate, e.Usage.CacheRead,
			e.CostUSD, e.RequestID)
		if err != nil {
			return 0, err
		}
		if rows, _ := res.RowsAffected(); rows > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

// TailOffset returns the persisted read offset for a transcript path
// (0 when the path has never been tailed).
func (s *Store) TailOffset(path string) (int64, error) {
	var off int64
	err := s.db.QueryRow(`SELECT offset FROM tail_state WHERE path = ?`, path).Scan(&off)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return off, err
}

// SetTailOffset persists the read offset for a transcript path.
func (s *Store) SetTailOffset(path string, offset int64) error {
	_, err := s.db.Exec(
		`INSERT INTO tail_state (path, offset) VALUES (?, ?)
		 ON CONFLICT(path) DO UPDATE SET offset = excluded.offset`, path, offset)
	return err
}

// SpentUSD returns the total cost of events in [from, to).
func (s *Store) SpentUSD(from, to time.Time) (float64, error) {
	var spent sql.NullFloat64
	err := s.db.QueryRow(
		`SELECT SUM(cost_usd) FROM usage_events WHERE ts >= ? AND ts < ?`,
		from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)).Scan(&spent)
	return spent.Float64, err
}

// WorkerSpentUSD returns one worker's cost in [from, to).
func (s *Store) WorkerSpentUSD(worker string, from, to time.Time) (float64, error) {
	var spent sql.NullFloat64
	err := s.db.QueryRow(
		`SELECT SUM(cost_usd) FROM usage_events WHERE worker = ? AND ts >= ? AND ts < ?`,
		worker, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)).Scan(&spent)
	return spent.Float64, err
}

// BurnRow is one session's spend within a window — a row of the "what is
// burning right now" view.
type BurnRow struct {
	SessionID string    `json:"session_id"`
	Worker    string    `json:"worker,omitempty"`
	Model     string    `json:"model"`
	CostUSD   float64   `json:"cost_usd"`
	Tokens    int64     `json:"tokens"`
	Events    int       `json:"events"`
	LastSeen  time.Time `json:"last_seen"`
}

// Burning answers "what is burning right now" in one query: per-session
// spend within [from, to), most expensive first.
func (s *Store) Burning(from, to time.Time) ([]BurnRow, error) {
	rows, err := s.db.Query(`
		SELECT session_id, MAX(worker), MAX(model), SUM(cost_usd),
		       SUM(input_tokens + output_tokens + cache_create_tokens + cache_read_tokens),
		       COUNT(*), MAX(ts)
		FROM usage_events WHERE ts >= ? AND ts < ?
		GROUP BY session_id ORDER BY SUM(cost_usd) DESC`,
		from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BurnRow
	for rows.Next() {
		var r BurnRow
		var last string
		if err := rows.Scan(&r.SessionID, &r.Worker, &r.Model, &r.CostUSD, &r.Tokens, &r.Events, &last); err != nil {
			return nil, err
		}
		r.LastSeen, _ = time.Parse(time.RFC3339Nano, last)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ActiveSessionCount returns the number of distinct sessions with
// billable activity in [from, to) — the fleet-size runaway signal.
func (s *Store) ActiveSessionCount(from, to time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT session_id) FROM usage_events WHERE ts >= ? AND ts < ?`,
		from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n, err
}
