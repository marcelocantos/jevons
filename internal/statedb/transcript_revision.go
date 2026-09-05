// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package statedb

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"time"
)

// A revision also records that even a zero-row transcript is authoritative.
// Existing schema_meta accommodates this additive state without a DDL change.
const transcriptRevisionPrefix = "transcript_revision:"

// TranscriptSnapshot reads one consistent journal revision and its events.
// Revision zero denotes a journal written before revision tracking, or a new
// journal. Its first mutation advances to one; revisions distinguish reused IDs.
type TranscriptSnapshot struct {
	Revision int64
	Events   []Event
}

type transcriptQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func transcriptRevision(q transcriptQuerier, agent string) (int64, error) {
	var value string
	err := q.QueryRow(`SELECT value FROM schema_meta WHERE key = ?`, transcriptRevisionPrefix+agent).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("statedb: transcript revision: %w", err)
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 1 {
		return 0, fmt.Errorf("statedb: invalid transcript revision for %q: %q", agent, value)
	}
	return revision, nil
}

func advanceTranscriptRevision(tx *sql.Tx, agent string) error {
	revision, err := transcriptRevision(tx, agent)
	if err != nil {
		return err
	}
	if revision == math.MaxInt64 {
		return fmt.Errorf("statedb: transcript revision exhausted for %q", agent)
	}
	_, err = tx.Exec(`INSERT INTO schema_meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		transcriptRevisionPrefix+agent, strconv.FormatInt(revision+1, 10))
	return err
}

func shouldImportTranscript(q transcriptQuerier, agent string) (bool, error) {
	var initialized bool
	err := q.QueryRow(`SELECT
		EXISTS (SELECT 1 FROM schema_meta WHERE key = ?)
		OR EXISTS (SELECT 1 FROM transcript_events WHERE agent = ?)
		OR EXISTS (SELECT 1 FROM import_watermark WHERE agent = ?)`,
		transcriptRevisionPrefix+agent, agent, agent).Scan(&initialized)
	if err != nil {
		return false, fmt.Errorf("statedb: transcript initialization: %w", err)
	}
	return !initialized, nil
}

// Snapshot returns rows and revision from the same SQLite read transaction.
// A tail index is not a revision: replacement can reuse every index and ID.
func (s *Store) Snapshot(agent string) (TranscriptSnapshot, error) {
	if s == nil {
		return TranscriptSnapshot{}, fmt.Errorf("statedb: no store for transcript snapshot")
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if err != nil {
		return TranscriptSnapshot{}, err
	}
	defer tx.Rollback()
	revision, err := transcriptRevision(tx, agent)
	if err != nil {
		return TranscriptSnapshot{}, err
	}
	rows, err := tx.Query(`SELECT idx, id, ts, typ, kind, body
		FROM transcript_events
		WHERE agent = ?
		ORDER BY idx`, agent)
	if err != nil {
		return TranscriptSnapshot{}, err
	}
	events, err := scanEvents(rows)
	closeErr := rows.Close()
	if err != nil {
		return TranscriptSnapshot{}, err
	}
	if closeErr != nil {
		return TranscriptSnapshot{}, closeErr
	}
	if err := tx.Commit(); err != nil {
		return TranscriptSnapshot{}, err
	}
	return TranscriptSnapshot{Revision: revision, Events: events}, nil
}

// ImportTranscript installs a legacy journal only if canonical state is still
// uninitialized. The check and rows/watermark/revision commit are atomic, so a
// live write during the caller's file read wins instead of being overwritten.
// False means there was already authoritative state; it is not an import error.
func (s *Store) ImportTranscript(agent, path string, size int64, events []Event) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("statedb: no store for transcript import")
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	shouldImport, err := shouldImportTranscript(tx, agent)
	if err != nil || !shouldImport {
		return false, err
	}
	if err := insertTranscriptRows(tx, agent, events, false); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`INSERT INTO import_watermark
		(agent, jsonl_path, jsonl_size, imported_n, imported_at)
		VALUES (?, ?, ?, ?, ?)`, agent, path, size, len(events), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return false, fmt.Errorf("statedb: import watermark: %w", err)
	}
	if err := advanceTranscriptRevision(tx, agent); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
