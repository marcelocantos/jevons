// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marcelocantos/claudia/controlplane"
)

// jControlPlaneLifecycle is the 🎯T1074.9 live clause. A cursor overseer
// turn makes the broker publish usage and a receipt in its own control
// file; they reach jevonsd's own replica over the control socket. Each file
// is open in exactly one process, its owner, before and after the restart.
// After the Jevons process restarts, that same session is still the seat,
// and the replica and the canonical usage count match the pre-restart facts.
func (s *suite) jControlPlaneLifecycle() error {
	if s.broker == nil {
		return fmt.Errorf("J44 needs the suite's own Claudia broker (provider %s runs direct)", s.provider)
	}
	session, err := s.waitOverseerSession(overseerName, 2*time.Minute)
	if err != nil {
		return err
	}
	if err := s.requireControlTopology(); err != nil {
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
	// The reply paints before the turn ends: the seat can make one more model
	// call after the token (gate 1a06d7ed counted 11, then the turn's last
	// call landed 2.8 s later and read as a restart-made row). The baseline is
	// taken once the overseer is idle and its usage count has stopped moving,
	// so the comparison after the restart measures the restart alone.
	if err := s.waitAgentPhase(overseerName, func(p string) bool { return p == "idle" }, overseerIdleBefore); err != nil {
		return fmt.Errorf("lifecycle turn did not end: %w", err)
	}
	before, err := s.waitControlFacts(overseerName, session, 20*time.Second)
	if err != nil {
		return err
	}
	calls, err := settledUsageCalls(s.stateDir, session, usageSettle, 20*time.Second)
	if err != nil {
		return err
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
	if err := s.requireControlTopology(); err != nil {
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

// requireControlTopology checks the two-database shape: the broker's file
// is open in the broker process alone, Jevons' replica in jevonsd alone,
// and neither process keeps a copy of the other's file in its own state.
func (s *suite) requireControlTopology() error {
	brokerPath := s.broker.controlFile()
	replicaPath := filepath.Join(s.stateDir, controlplane.JevonsDBFile)
	if _, err := os.Lstat(filepath.Join(s.stateDir, controlplane.ClaudiaDBFile)); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("jevons state holds the broker's file name %s: %v", controlplane.ClaudiaDBFile, err)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(brokerPath), controlplane.JevonsDBFile)); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("broker state holds Jevons' file name %s: %v", controlplane.JevonsDBFile, err)
	}
	for _, want := range []struct {
		path, owner string
		pid         int
	}{
		{brokerPath, "the broker", s.broker.cmd.Process.Pid},
		{replicaPath, "jevonsd", s.cmd.Process.Pid},
	} {
		pids, err := openingPIDs(want.path)
		if err != nil {
			return err
		}
		if len(pids) != 1 || pids[0] != want.pid {
			return fmt.Errorf("%s is open in pids %v; want %s (pid %d) alone", want.path, pids, want.owner, want.pid)
		}
	}
	return nil
}

// openingPIDs lists the processes holding path open, by lsof. lsof exits 1
// when no process has it open; that is an empty list, not an error.
func openingPIDs(path string) ([]int, error) {
	out, err := exec.Command("lsof", "-t", "--", path).Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1 && len(out) == 0) {
		return nil, fmt.Errorf("lsof %s: %w", path, err)
	}
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("lsof %s: pid %q", path, field)
		}
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	return pids, nil
}

func (s *suite) waitControlFacts(seat, session string, d time.Duration) (controlFacts, error) {
	brokerPath := s.broker.controlFile()
	replicaPath := filepath.Join(s.stateDir, controlplane.JevonsDBFile)
	deadline := time.Now().Add(d)
	var last error
	for {
		broker, berr := readControlCopy(brokerPath, seat, session)
		replica, rerr := readControlCopy(replicaPath, seat, session)
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

// readControlCopy reads facts from a byte copy of a control file. The
// journey never opens a live control file as a database: each has one
// owner. Neither file uses WAL, so the main file is the whole database
// between its owner's transactions; a copy torn by a commit fails to read
// and the caller polls again.
func readControlCopy(path, seat, session string) (controlFacts, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return controlFacts{}, err
	}
	dir, err := os.MkdirTemp("", "j44-control-")
	if err != nil {
		return controlFacts{}, err
	}
	defer os.RemoveAll(dir)
	copyPath := filepath.Join(dir, filepath.Base(path))
	if err := os.WriteFile(copyPath, body, 0o600); err != nil {
		return controlFacts{}, err
	}
	return readControlFacts(copyPath, seat, session)
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
	// The cost collector writes this database while J44 reads it. Without a
	// busy timeout a read that meets a write fails at once with SQLITE_BUSY
	// (the 2026-10-29 sharded gate), as the daemon's own stores already
	// guard against (internal/cost/store.go).
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return 0, err
	}
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM usage_calls WHERE session_id = ?`, session).Scan(&n)
	return n, err
}

// usageSettle is how long the canonical usage count must hold still before
// J44 takes it as the pre-restart baseline: a fact replicates from the
// broker after the turn that made it ends.
const usageSettle = 2 * time.Second

// settledUsageCalls waits for at least one canonical row before settling a
// baseline. The control-plane file can replicate before the cost collector's
// next three-second poll: zero held for usageSettle is not a stable usage
// baseline. An absent row remains a failure when the bounded wait expires.
func settledUsageCalls(stateDir, session string, settle, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	prev, err := canonicalUsageCalls(stateDir, session)
	if err != nil {
		return 0, err
	}
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, fmt.Errorf("canonical usage calls for %s did not settle after %s (last %d)", session, timeout, prev)
		}
		// A zero count has not yet reached the cost collector. Poll it
		// promptly; only a positive count earns the full settling period.
		pause := settle
		if prev == 0 {
			pause = min(settle, 100*time.Millisecond)
		}
		time.Sleep(min(pause, remaining))
		cur, err := canonicalUsageCalls(stateDir, session)
		if err != nil {
			return 0, err
		}
		if cur > 0 && cur == prev && pause == settle {
			return cur, nil
		}
		prev = cur
	}
}
