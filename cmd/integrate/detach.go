// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

// 🎯T1259: a landing outlives a worker's shell command. The post-merge gate
// runs scripts/docratchet (~11 min under load, 🎯T808) plus the touched
// packages, and a worker seat's Bash call is capped at 600 s, so on
// 2026-10-28 jv-t1254-stop-unknown's integrate passed its gate twice and was
// killed both times, and every finished worker queued behind its product
// owner to land by hand. -detach starts the same landing in its own session
// with its output in a log, and -result AGENT reports how it ended, with the
// landing's own exit status, so a worker can poll across several commands.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// resultEnv names the record file a detached child writes; logEnv its log.
// Only the child writes the record (running first, then its outcome), so a
// refusal that ends in milliseconds cannot be overwritten by its parent.
const (
	resultEnv = "JEVONS_INTEGRATE_RESULT"
	logEnv    = "JEVONS_INTEGRATE_LOG"
)

// exitRunning is -result's status while the landing has not finished.
const exitRunning = 3

type detachRecord struct {
	Agents     []string  `json:"agents"`
	PID        int       `json:"pid"`
	Log        string    `json:"log"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	Exit       *int      `json:"exit,omitempty"`
}

// integrateDir lives inside the gate store because every sandboxed work seat
// may write there (🎯T386) and nowhere else under ~/.jevons: on 2026-10-28
// pigeon-po's detached landings were refused write access to
// ~/.jevons/integrate. A landing record is gate evidence anyway.
func integrateDir() string {
	if d := os.Getenv("JEVONS_INTEGRATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(gate.DefaultStoreRoot(), "integrate")
}

// detachStartGrace is how long a record may say "starting" before -result
// calls the landing dead: the child writes its own PID within moments.
const detachStartGrace = time.Minute

// recordPath is per first agent: -result names an agent, and one landing at
// a time per worker is the only shape the fleet uses.
func recordPath(agent string) string {
	return filepath.Join(integrateDir(), agent+".json")
}

func startDetached(agents []string) int {
	dir := integrateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "integrate: %v\n", err)
		return 2
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	logPath := filepath.Join(dir, agents[0]+"-"+stamp+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integrate: %v\n", err)
		return 2
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "integrate: %v\n", err)
		return 2
	}
	var args []string
	for _, a := range os.Args[1:] {
		if a == "-detach" || a == "--detach" || strings.HasPrefix(a, "-detach=") || strings.HasPrefix(a, "--detach=") {
			continue
		}
		args = append(args, a)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), resultEnv+"="+recordPath(agents[0]), logEnv+"="+logPath)
	// Its own session: the worker's shell call ending does not take it down.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The record exists before this returns (🎯T1259). Left to the child, a
	// -result straight after -detach found no record, or the previous
	// landing's finished one, until the child got round to writing; the
	// gate's load made that a test failure. PID 0 is "starting".
	record := recordPath(agents[0])
	if err := writeRecord(record, detachRecord{Agents: agents, Log: logPath, StartedAt: time.Now().UTC()}); err != nil {
		fmt.Fprintf(os.Stderr, "integrate: record start: %v\n", err)
		return 2
	}
	if err := cmd.Start(); err != nil {
		code := 2
		_ = writeRecord(record, detachRecord{Agents: agents, Log: logPath, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), Exit: &code})
		fmt.Fprintf(os.Stderr, "integrate: start detached: %v\n", err)
		return 2
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	// Name the child unless it already wrote its own record.
	if rec, err := readRecord(record); err == nil && rec.PID == 0 && rec.Exit == nil {
		rec.PID = pid
		_ = writeRecord(record, rec)
	}
	fmt.Printf("INTEGRATE detached pid=%d log=%s\n", pid, logPath)
	fmt.Printf("poll: go run ./cmd/integrate -result %s   (exit 0 landed, 1 refused, %d still running)\n", agents[0], exitRunning)
	return 0
}

// recordStart writes the running record when this process is a detached child.
func recordStart(agents []string) {
	path := os.Getenv(resultEnv)
	if path == "" {
		return
	}
	rec := detachRecord{Agents: agents, PID: os.Getpid(), Log: os.Getenv(logEnv), StartedAt: time.Now().UTC()}
	if err := writeRecord(path, rec); err != nil {
		fmt.Fprintf(os.Stderr, "integrate: record start: %v\n", err)
	}
}

// recordResult finishes the record when this process is a detached child.
func recordResult(agents []string, code int) {
	path := os.Getenv(resultEnv)
	if path == "" {
		return
	}
	rec, err := readRecord(path)
	if err != nil {
		rec = detachRecord{Agents: agents, PID: os.Getpid(), Log: os.Getenv(logEnv)}
	}
	rec.FinishedAt = time.Now().UTC()
	rec.Exit = &code
	if err := writeRecord(path, rec); err != nil {
		fmt.Fprintf(os.Stderr, "integrate: record result: %v\n", err)
	}
}

func printResult(agent string) int {
	rec, err := readRecord(recordPath(agent))
	if errors.Is(err, os.ErrNotExist) && os.Getenv("JEVONS_INTEGRATE_DIR") == "" {
		// Landings started before the records moved into the gate store.
		home, _ := os.UserHomeDir()
		rec, err = readRecord(filepath.Join(home, ".jevons", "integrate", agent+".json"))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "integrate: no detached landing recorded for %s: %v\n", agent, err)
		return 2
	}
	tail := logTail(rec.Log, 20)
	if rec.Exit == nil && rec.PID == 0 {
		// Written by -detach before its child started (🎯T1259). Kill(0, 0)
		// would test our own process group, so it is judged by age instead.
		if time.Since(rec.StartedAt) > detachStartGrace {
			fmt.Printf("INTEGRATE %s never started (recorded %s); log %s\n%s",
				agent, rec.StartedAt.Format(time.RFC3339), rec.Log, tail)
			return 1
		}
		fmt.Printf("INTEGRATE %s starting; log %s\n%s", agent, rec.Log, tail)
		return exitRunning
	}
	if rec.Exit == nil {
		if syscall.Kill(rec.PID, 0) != nil {
			fmt.Printf("INTEGRATE %s died without a result (pid %d, started %s); log %s\n%s",
				agent, rec.PID, rec.StartedAt.Format(time.RFC3339), rec.Log, tail)
			return 1
		}
		fmt.Printf("INTEGRATE %s running pid=%d for %s; log %s\n%s",
			agent, rec.PID, time.Since(rec.StartedAt).Round(time.Second), rec.Log, tail)
		return exitRunning
	}
	verdict := "LANDED"
	if *rec.Exit != 0 {
		verdict = "REFUSED"
	}
	fmt.Printf("INTEGRATE %s %s exit=%d after %s; log %s\n%s",
		agent, verdict, *rec.Exit, rec.FinishedAt.Sub(rec.StartedAt).Round(time.Second), rec.Log, tail)
	return *rec.Exit
}

func logTail(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

func readRecord(path string) (detachRecord, error) {
	var rec detachRecord
	data, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, fmt.Errorf("malformed %s: %w", path, err)
	}
	return rec, nil
}

// writeRecord is write-and-rename: a reader never sees half a record.
func writeRecord(path string, rec detachRecord) error {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".integrate-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
