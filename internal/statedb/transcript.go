// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package statedb

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

// Event is one coalesced transcript row. Index is journal-absolute
// (1-based). Body is the event JSON the mux already fans.
type Event struct {
	Index int
	ID    string
	TS    string
	Type  string
	Kind  int
	Body  string
}

// Watermark records a completed JSONL import for one agent.
type Watermark struct {
	Agent      string
	JSONLPath  string
	JSONLSize  int64
	ImportedN  int
	ImportedAt string
}

// TailStart is the 1-based idx of the oldest event in the last userTurns
// user rows. Vanilla /ws/chat first-paint is historyReplayTurns (30)
// sealed turns, not 30 raw events (🎯T548.2 / T57). No user rows → last
// userTurns events, or 1 when the journal is shorter.
func (s *Store) TailStart(agent string, userTurns int) (int, error) {
	if s == nil {
		return 1, nil
	}
	n, err := s.N(agent)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 1, nil
	}
	if userTurns <= 0 {
		return n, nil
	}
	var lo sql.NullInt64
	err = s.db.QueryRow(`
		SELECT MIN(idx) FROM (
			SELECT idx FROM transcript_events
			WHERE agent = ? AND typ = 'user'
			ORDER BY idx DESC LIMIT ?
		)`, agent, userTurns).Scan(&lo)
	if err != nil {
		return 0, fmt.Errorf("statedb: tail-start: %w", err)
	}
	if lo.Valid {
		return int(lo.Int64), nil
	}
	start := n - userTurns + 1
	if start < 1 {
		start = 1
	}
	return start, nil
}

// N is the journal-absolute length: MAX(idx), or 0 when empty.
func (s *Store) N(agent string) (int, error) {
	if s == nil {
		return 0, nil
	}
	var n sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(idx) FROM transcript_events WHERE agent = ?`, agent).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("statedb: n: %w", err)
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64), nil
}

// Count is the number of stored rows (holes make this differ from N).
func (s *Store) Count(agent string) (int, error) {
	if s == nil {
		return 0, nil
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM transcript_events WHERE agent = ?`, agent).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("statedb: count: %w", err)
	}
	return n, nil
}

// Range returns events with lo <= idx < hi, in index order.
func (s *Store) Range(agent string, lo, hi int) ([]Event, error) {
	if s == nil || hi <= lo {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT idx, id, ts, typ, kind, body FROM transcript_events
		 WHERE agent = ? AND idx >= ? AND idx < ? ORDER BY idx`,
		agent, lo, hi,
	)
	if err != nil {
		return nil, fmt.Errorf("statedb: range: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// Before returns up to limit events with idx < before, oldest first.
func (s *Store) Before(agent string, before, limit int) ([]Event, error) {
	if s == nil || before < 2 || limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT idx, id, ts, typ, kind, body FROM transcript_events
		 WHERE agent = ? AND idx < ? ORDER BY idx DESC LIMIT ?`,
		agent, before, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("statedb: before: %w", err)
	}
	defer rows.Close()
	desc, err := scanEvents(rows)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(desc)-1; i < j; i, j = i+1, j-1 {
		desc[i], desc[j] = desc[j], desc[i]
	}
	return desc, nil
}

func scanEvents(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.Index, &ev.ID, &ev.TS, &ev.Type, &ev.Kind, &ev.Body); err != nil {
			return nil, fmt.Errorf("statedb: scan: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// Upsert inserts or replaces events by (agent, idx).
func (s *Store) Upsert(agent string, evs []Event) error {
	if s == nil || len(evs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO transcript_events
		(agent, idx, id, ts, typ, kind, body) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, ev := range evs {
		if ev.Index < 1 {
			continue
		}
		id := ev.ID
		if id == "" {
			id = fmt.Sprintf("e:%d", ev.Index)
		}
		if _, err := stmt.Exec(agent, ev.Index, id, ev.TS, ev.Type, ev.Kind, ev.Body); err != nil {
			return fmt.Errorf("statedb: upsert idx=%d: %w", ev.Index, err)
		}
	}
	return tx.Commit()
}

// ReplaceAll deletes an agent's rows and inserts evs in one transaction.
func (s *Store) ReplaceAll(agent string, evs []Event) error {
	if s == nil {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM transcript_events WHERE agent = ?`, agent); err != nil {
		return fmt.Errorf("statedb: replace delete: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO transcript_events
		(agent, idx, id, ts, typ, kind, body) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, ev := range evs {
		if ev.Index < 1 {
			continue
		}
		id := ev.ID
		if id == "" {
			id = fmt.Sprintf("e:%d", ev.Index)
		}
		if _, err := stmt.Exec(agent, ev.Index, id, ev.TS, ev.Type, ev.Kind, ev.Body); err != nil {
			return fmt.Errorf("statedb: replace idx=%d: %w", ev.Index, err)
		}
	}
	return tx.Commit()
}

// GetWatermark returns the last import record, or nil when none.
func (s *Store) GetWatermark(agent string) (*Watermark, error) {
	if s == nil {
		return nil, nil
	}
	var w Watermark
	err := s.db.QueryRow(
		`SELECT agent, jsonl_path, jsonl_size, imported_n, imported_at
		 FROM import_watermark WHERE agent = ?`, agent,
	).Scan(&w.Agent, &w.JSONLPath, &w.JSONLSize, &w.ImportedN, &w.ImportedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("statedb: watermark: %w", err)
	}
	return &w, nil
}

// SetWatermark records a completed import.
func (s *Store) SetWatermark(agent, jsonlPath string, jsonlSize int64, importedN int) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO import_watermark
		 (agent, jsonl_path, jsonl_size, imported_n, imported_at)
		 VALUES (?,?,?,?,?)`,
		agent, jsonlPath, jsonlSize, importedN, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("statedb: set watermark: %w", err)
	}
	return nil
}

// ShouldImport is true when this agent has no coalesced rows yet.
func (s *Store) ShouldImport(agent string) (bool, error) {
	n, err := s.N(agent)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// JSONLSize is the current size of path, or 0 when missing.
func JSONLSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}
