// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package workers

import (
	"fmt"
	"strings"
	"time"
)

// Tracker is the high-level worker observability surface: SQLite + SSE hub.
type Tracker struct {
	Store *Store
	Hub   *Hub
}

// NewTracker opens path and attaches a hub. Path may be ":memory:".
func NewTracker(path string) (*Tracker, error) {
	st, err := Open(path)
	if err != nil {
		return nil, err
	}
	return &Tracker{Store: st, Hub: NewHub()}, nil
}

// Close closes the underlying store.
func (t *Tracker) Close() error {
	if t == nil || t.Store == nil {
		return nil
	}
	return t.Store.Close()
}

// StartArgs is the input for recording a new worker.
type StartArgs struct {
	ID    string
	Task  string
	Model string
	Cwd   string
}

// Start records a running worker and publishes worker_started.
func (t *Tracker) Start(args StartArgs) error {
	if t == nil || t.Store == nil {
		return fmt.Errorf("workers tracker not configured")
	}
	w := &Worker{
		ID:        args.ID,
		Task:      args.Task,
		Status:    StatusRunning,
		Model:     args.Model,
		Cwd:       args.Cwd,
		StartedAt: time.Now().UTC(),
	}
	if err := t.Store.InsertWorker(w); err != nil {
		return err
	}
	if t.Hub != nil {
		t.Hub.Publish(HubEvent{
			Type:     "worker_started",
			WorkerID: args.ID,
			Status:   StatusRunning,
			Task:     truncate(args.Task, 200),
			Model:    args.Model,
		})
	}
	return nil
}

// Progress appends an output line and publishes worker_progress.
func (t *Tracker) Progress(workerID, line string) error {
	if t == nil || t.Store == nil {
		return nil
	}
	line = strings.TrimRight(line, "\n")
	if line == "" {
		return nil
	}
	if _, err := t.Store.AppendEvent(workerID, line); err != nil {
		return err
	}
	if t.Hub != nil {
		t.Hub.Publish(HubEvent{
			Type:     "worker_progress",
			WorkerID: workerID,
			Line:     truncate(line, 500),
		})
	}
	return nil
}

// PolicyArgs carries 🎯T8.3 policy fields onto a worker row + stream.
type PolicyArgs struct {
	Decision string
	Level    int
	Reason   string
	RuleID   string
	AuditSeq uint64
}

// RecordPolicy stores policy metadata and re-broadcasts if the worker is live.
func (t *Tracker) RecordPolicy(workerID string, p PolicyArgs) error {
	if t == nil || t.Store == nil {
		return nil
	}
	if err := t.Store.SetPolicy(workerID, p.Decision, p.Reason, p.RuleID, p.Level, p.AuditSeq); err != nil {
		return err
	}
	return nil
}

// FinishArgs completes a worker with outcome and optional token totals.
type FinishArgs struct {
	ID           string
	Status       string // completed | failed | denied
	Outcome      string
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	Policy       *PolicyArgs
}

// Finish marks the worker done, optionally records policy, and publishes.
func (t *Tracker) Finish(args FinishArgs) error {
	if t == nil || t.Store == nil {
		return nil
	}
	status := args.Status
	if status == "" {
		status = StatusCompleted
	}
	if args.Policy != nil {
		_ = t.Store.SetPolicy(args.ID, args.Policy.Decision, args.Policy.Reason,
			args.Policy.RuleID, args.Policy.Level, args.Policy.AuditSeq)
	}
	if err := t.Store.Complete(args.ID, status, args.Outcome,
		args.InputTokens, args.OutputTokens, args.CostUSD); err != nil {
		return err
	}
	if t.Hub != nil {
		evType := "worker_completed"
		switch status {
		case StatusFailed:
			evType = "worker_failed"
		case StatusDenied:
			evType = "worker_denied"
		}
		he := HubEvent{
			Type:     evType,
			WorkerID: args.ID,
			Status:   status,
			Outcome:  truncate(args.Outcome, 300),
		}
		if args.Policy != nil {
			he.PolicyDecision = args.Policy.Decision
			he.PolicyLevel = args.Policy.Level
		}
		t.Hub.Publish(he)
	}
	return nil
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
