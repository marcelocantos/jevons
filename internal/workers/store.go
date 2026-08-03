// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package workers tracks ephemeral jwork (and related) worker lifecycle in
// SQLite and fans lifecycle events to an SSE hub for the dashboard (🎯T8.2).
package workers

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo
)

// Status values for workers.status.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusDenied    = "denied" // policy denied before spawn
)

// Worker is one tracked worker row (🎯T8.2 schema + token/outcome extensions).
type Worker struct {
	ID             string     `json:"id"`
	Task           string     `json:"task"`
	Status         string     `json:"status"`
	Model          string     `json:"model,omitempty"`
	Cwd            string     `json:"cwd,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	Outcome        string     `json:"outcome,omitempty"`
	InputTokens    int64      `json:"input_tokens"`
	OutputTokens   int64      `json:"output_tokens"`
	CostUSD        float64    `json:"cost_usd"`
	PolicyDecision string     `json:"policy_decision,omitempty"`
	PolicyLevel    int        `json:"policy_level,omitempty"`
	PolicyReason   string     `json:"policy_reason,omitempty"`
	PolicyRuleID   string     `json:"policy_rule_id,omitempty"`
	AuditSeq       uint64     `json:"audit_seq,omitempty"`
}

// Event is one worker output line (or lifecycle note) stored for replay.
type Event struct {
	ID       int64     `json:"id"`
	WorkerID string    `json:"worker_id"`
	TS       time.Time `json:"ts"`
	Line     string    `json:"line"`
}

// Store persists workers + events. Safe for concurrent use (single connection).
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS workers (
	id              TEXT PRIMARY KEY,
	task            TEXT NOT NULL,
	status          TEXT NOT NULL,
	model           TEXT NOT NULL DEFAULT '',
	cwd             TEXT NOT NULL DEFAULT '',
	started_at      TEXT NOT NULL,
	ended_at        TEXT,
	outcome         TEXT NOT NULL DEFAULT '',
	input_tokens    INTEGER NOT NULL DEFAULT 0,
	output_tokens   INTEGER NOT NULL DEFAULT 0,
	cost_usd        REAL NOT NULL DEFAULT 0,
	policy_decision TEXT NOT NULL DEFAULT '',
	policy_level    INTEGER NOT NULL DEFAULT 0,
	policy_reason   TEXT NOT NULL DEFAULT '',
	policy_rule_id  TEXT NOT NULL DEFAULT '',
	audit_seq       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_workers_status ON workers(status);
CREATE INDEX IF NOT EXISTS idx_workers_started ON workers(started_at);

CREATE TABLE IF NOT EXISTS events (
	id         INTEGER PRIMARY KEY,
	worker_id  TEXT NOT NULL,
	ts         TEXT NOT NULL,
	line       TEXT NOT NULL,
	FOREIGN KEY (worker_id) REFERENCES workers(id)
);
CREATE INDEX IF NOT EXISTS idx_events_worker ON events(worker_id, id);
`

// Open opens (or creates) the workers database at path. Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open workers db: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create workers schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// InsertWorker records a new running worker.
func (s *Store) InsertWorker(w *Worker) error {
	if w == nil || strings.TrimSpace(w.ID) == "" {
		return fmt.Errorf("worker id required")
	}
	if w.Status == "" {
		w.Status = StatusRunning
	}
	if w.StartedAt.IsZero() {
		w.StartedAt = time.Now().UTC()
	}
	var ended any
	if w.EndedAt != nil {
		ended = w.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(`INSERT INTO workers
		(id, task, status, model, cwd, started_at, ended_at, outcome,
		 input_tokens, output_tokens, cost_usd,
		 policy_decision, policy_level, policy_reason, policy_rule_id, audit_seq)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.Task, w.Status, w.Model, w.Cwd,
		w.StartedAt.UTC().Format(time.RFC3339Nano), ended, w.Outcome,
		w.InputTokens, w.OutputTokens, w.CostUSD,
		w.PolicyDecision, w.PolicyLevel, w.PolicyReason, w.PolicyRuleID, w.AuditSeq)
	return err
}

// AppendEvent stores one output line for a worker.
func (s *Store) AppendEvent(workerID, line string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO events (worker_id, ts, line) VALUES (?,?,?)`,
		workerID, time.Now().UTC().Format(time.RFC3339Nano), line)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Complete marks a worker finished with outcome, optional token/cost totals,
// and policy metadata.
func (s *Store) Complete(id, status, outcome string, inputTok, outputTok int64, costUSD float64) error {
	if status == "" {
		status = StatusCompleted
	}
	_, err := s.db.Exec(`UPDATE workers SET
		status = ?, ended_at = ?, outcome = ?,
		input_tokens = ?, output_tokens = ?, cost_usd = ?
		WHERE id = ?`,
		status, time.Now().UTC().Format(time.RFC3339Nano), outcome,
		inputTok, outputTok, costUSD, id)
	return err
}

// SetPolicy records the policy decision associated with a worker.
func (s *Store) SetPolicy(id, decision, reason, ruleID string, level int, auditSeq uint64) error {
	_, err := s.db.Exec(`UPDATE workers SET
		policy_decision = ?, policy_level = ?, policy_reason = ?,
		policy_rule_id = ?, audit_seq = ?
		WHERE id = ?`,
		decision, level, reason, ruleID, auditSeq, id)
	return err
}

// Get returns one worker by id, or nil if missing.
func (s *Store) Get(id string) (*Worker, error) {
	row := s.db.QueryRow(`SELECT id, task, status, model, cwd, started_at, ended_at,
		outcome, input_tokens, output_tokens, cost_usd,
		policy_decision, policy_level, policy_reason, policy_rule_id, audit_seq
		FROM workers WHERE id = ?`, id)
	return scanWorker(row)
}

// List returns workers newest-first. statusFilter empty = all; limit <= 0 = 100.
func (s *Store) List(statusFilter string, limit int) ([]Worker, error) {
	if limit <= 0 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if statusFilter == "" {
		rows, err = s.db.Query(`SELECT id, task, status, model, cwd, started_at, ended_at,
			outcome, input_tokens, output_tokens, cost_usd,
			policy_decision, policy_level, policy_reason, policy_rule_id, audit_seq
			FROM workers ORDER BY started_at DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(`SELECT id, task, status, model, cwd, started_at, ended_at,
			outcome, input_tokens, output_tokens, cost_usd,
			policy_decision, policy_level, policy_reason, policy_rule_id, audit_seq
			FROM workers WHERE status = ? ORDER BY started_at DESC LIMIT ?`,
			statusFilter, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Worker
	for rows.Next() {
		w, err := scanWorkerRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// Events returns output lines for a worker in order (after afterID when > 0).
func (s *Store) Events(workerID string, afterID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT id, worker_id, ts, line FROM events
		WHERE worker_id = ? AND id > ? ORDER BY id ASC LIMIT ?`,
		workerID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var ts string
		if err := rows.Scan(&e.ID, &e.WorkerID, &ts, &e.Line); err != nil {
			return nil, err
		}
		e.TS, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, e)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanWorker(row scannable) (*Worker, error) {
	var w Worker
	var started, ended sql.NullString
	err := row.Scan(
		&w.ID, &w.Task, &w.Status, &w.Model, &w.Cwd, &started, &ended,
		&w.Outcome, &w.InputTokens, &w.OutputTokens, &w.CostUSD,
		&w.PolicyDecision, &w.PolicyLevel, &w.PolicyReason, &w.PolicyRuleID, &w.AuditSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if started.Valid {
		w.StartedAt, _ = time.Parse(time.RFC3339Nano, started.String)
	}
	if ended.Valid && ended.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, ended.String)
		w.EndedAt = &t
	}
	return &w, nil
}

func scanWorkerRows(rows *sql.Rows) (*Worker, error) {
	return scanWorker(rows)
}
