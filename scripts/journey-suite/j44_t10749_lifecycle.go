// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marcelocantos/claudia/controlplane"
)

// jControlPlaneLifecycle is the 🎯T1074.9 live clause. A cursor overseer
// turn publishes usage and a receipt on the shared control plane. After
// the Jevons process restarts, that same session is still the seat, and
// the replica and the canonical usage count match the pre-restart facts.
func (s *suite) jControlPlaneLifecycle() error {
	session, err := s.waitOverseerSession(overseerName, 2*time.Minute)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	conn, frames, err := dialOwnerMux(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := collectOwnerMuxReplay(ctx, frames); err != nil {
		return err
	}
	token := "journey-lifecycle-" + uuid.NewString()
	prompt := "Reply with exactly: " + token
	if err := writeOwnerMux(ctx, conn, "send", map[string]string{"text": prompt}); err != nil {
		return err
	}
	if err := waitOwnerMuxReply(ctx, frames, prompt, token); err != nil {
		return fmt.Errorf("lifecycle turn: %w", err)
	}
	before, err := s.waitControlFacts(overseerName, session, 20*time.Second)
	if err != nil {
		return err
	}
	calls, err := canonicalUsageCalls(s.stateDir, session)
	if err != nil {
		return err
	}
	if calls < 1 {
		return fmt.Errorf("canonical usage calls for %s = 0", session)
	}
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("jevons restart: %w", err)
	}
	if err := s.requireOverseerSession(overseerName, session); err != nil {
		return fmt.Errorf("after restart: %w", err)
	}
	after, err := s.waitControlFacts(overseerName, session, 20*time.Second)
	if err != nil {
		return fmt.Errorf("after restart: %w", err)
	}
	if !before.equal(after) {
		return fmt.Errorf("control plane changed across the restart: before %s after %s", before, after)
	}
	callsAfter, err := canonicalUsageCalls(s.stateDir, session)
	if err != nil {
		return err
	}
	if callsAfter != calls {
		return fmt.Errorf("canonical usage calls = %d after restart, was %d", callsAfter, calls)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	return queueJourneyProvider(logs, overseerName, string(s.provider))
}

// controlFacts is one seat session's usage-row count and receipt states.
// It is the observation of the files the daemon and the broker already wrote.
type controlFacts struct {
	usage    int
	receipts []string
}

func (f controlFacts) equal(o controlFacts) bool {
	if f.usage != o.usage || len(f.receipts) != len(o.receipts) {
		return false
	}
	for i := range f.receipts {
		if f.receipts[i] != o.receipts[i] {
			return false
		}
	}
	return true
}

func (f controlFacts) String() string {
	return fmt.Sprintf("usage=%d receipts=%s", f.usage, strings.Join(f.receipts, ","))
}

// terminal is true when a receipt has left the in-flight states. accepted
// and started are not a finished turn. unknown is not a finished turn.
func (f controlFacts) terminal() bool {
	for _, row := range f.receipts {
		_, state, ok := strings.Cut(row, " ")
		if !ok {
			continue
		}
		switch state {
		case "completed", "cancelled", "failed":
			return true
		}
	}
	return false
}

func (s *suite) waitControlFacts(seat, session string, d time.Duration) (controlFacts, error) {
	brokerPath := filepath.Join(s.stateDir, controlplane.ClaudiaDBFile)
	replicaPath := filepath.Join(s.stateDir, controlplane.JevonsDBFile)
	deadline := time.Now().Add(d)
	var last error
	for {
		broker, berr := readControlFacts(brokerPath, seat, session)
		replica, rerr := readControlFacts(replicaPath, seat, session)
		switch {
		case berr != nil:
			last = fmt.Errorf("broker file: %w", berr)
		case rerr != nil:
			last = fmt.Errorf("replica: %w", rerr)
		case !broker.equal(replica):
			last = fmt.Errorf("replica has not caught the broker: broker %s replica %s", broker, replica)
		case broker.usage < 1 || !broker.terminal():
			last = fmt.Errorf("turn has not published a terminal receipt: %s", broker)
		default:
			return broker, nil
		}
		if time.Now().After(deadline) {
			return controlFacts{}, fmt.Errorf("control plane did not converge for %s session %s: %w", seat, session, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func readControlFacts(path, seat, session string) (controlFacts, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return controlFacts{}, err
	}
	uri := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return controlFacts{}, err
	}
	defer db.Close()
	var facts controlFacts
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM claudia_usage_fact WHERE seat = ? AND session_id = ?`,
		seat, session,
	).Scan(&facts.usage); err != nil {
		return controlFacts{}, err
	}
	rows, err := db.Query(
		`SELECT request_id, state FROM claudia_request_receipt WHERE seat = ? AND session_id = ? ORDER BY request_id`,
		seat, session,
	)
	if err != nil {
		return controlFacts{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, state string
		if err := rows.Scan(&id, &state); err != nil {
			return controlFacts{}, err
		}
		facts.receipts = append(facts.receipts, id+" "+state)
	}
	if err := rows.Err(); err != nil {
		return controlFacts{}, err
	}
	sort.Strings(facts.receipts)
	return facts, nil
}

func canonicalUsageCalls(stateDir, session string) (int, error) {
	abs, err := filepath.Abs(filepath.Join(stateDir, "usage.db"))
	if err != nil {
		return 0, err
	}
	uri := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM usage_calls WHERE session_id = ?`, session).Scan(&n)
	return n, err
}
