// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/statedb"
)

// t1082Opener is the overseer's tool call from gate 252cf6ac, byte for byte
// (isolate transcript row jevons/40).
const t1082Opener = `{"id":"call-98fb2f3b-5baa-4bad-864b-7658cb941920-12|fc_8b809368-07b4-9ba6-baab-7f1bdcf305a3_1","input":{"command":"sqlite3 ~/.jevons/jevons.db \"SELECT name FROM sqlite_master WHERE type='table';\" 2>&1 | head -40"},"name":"Bash","timestamp":"2026-10-19T22:09:18.181036Z","type":"tool_use"}`

func seedToolRows(t *testing.T, stateDir string, rows map[string][2]string) {
	t.Helper()
	db, err := statedb.Open(statedb.DefaultPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for agent, row := range rows {
		if err := db.Upsert(agent, []statedb.Event{{Index: 1, ID: agent + "-1", Type: row[0], Body: row[1]}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestT1082ToolCallScanFindsTheDevelopmentOpener(t *testing.T) {
	home := "/Users/someone"
	state := t.TempDir()
	seedToolRows(t, state, map[string][2]string{
		"jevons":     {"tool_use", t1082Opener},
		"jv-po":      {"tool_use", `{"input":{"command":"ls -la $HOME/.jevons 2>/dev/null"},"type":"tool_use"}`},
		"jv-abs":     {"tool_use", `{"input":{"command":"cat /Users/someone/.jevons/agents.json"},"type":"tool_use"}`},
		"jv-sibling": {"tool_use", `{"input":{"command":"ls /Users/someone/.jevons-worktrees-jevons/x ~/.jevonsrc"},"type":"tool_use"}`},
		"jv-own":     {"tool_use", `{"input":{"command":"sqlite3 ` + state + `/jevons.db .tables"},"type":"tool_use"}`},
		"jv-prose":   {"assistant", `{"message":{"content":[{"text":"development state is ~/.jevons"}]}}`},
	})
	touches, err := scanDevStateRefs(state, home)
	if err != nil {
		t.Fatal(err)
	}
	var agents []string
	for _, tc := range touches {
		agents = append(agents, tc.agent)
	}
	got := strings.Join(agents, ",")
	if got != "jevons,jv-abs,jv-po" {
		t.Fatalf("touches = %v, want the opener, the absolute path and the $HOME listing only", touches)
	}
	if !strings.Contains(touches[0].String(), "sqlite3 ~/.jevons/jevons.db") {
		t.Fatalf("touch does not quote the call: %s", touches[0])
	}
	if none, err := scanDevStateRefs(t.TempDir(), home); err != nil || len(none) != 0 {
		t.Fatalf("absent store: %v %v", none, err)
	}
}

// The fingerprint half: a read-write open of the development database leaves
// a -wal and -shm behind (the 09:09 evidence) and fails the run.
func TestT1082GuardFailsWhenTheDevelopmentDatabaseIsOpened(t *testing.T) {
	home := t.TempDir()
	dev := filepath.Join(home, ".jevons")
	db, err := statedb.Open(statedb.DefaultPath(dev))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, sib := range []string{"-wal", "-shm"} {
		_ = os.Remove(statedb.DefaultPath(dev) + sib)
	}
	quiet := func() bool { return false }

	g, err := startDevStateGuardWith(home, os.Getpid(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.finish(t.TempDir()); err != nil {
		t.Fatalf("untouched development state failed the guard: %v", err)
	}

	g, err = startDevStateGuardWith(home, os.Getpid(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := statedb.Open(statedb.DefaultPath(dev))
	if err != nil {
		t.Fatal(err)
	}
	// Held long enough for the holder poll, as a daemon that resolved the
	// default state dir would hold it for its life.
	time.Sleep(3 * devStatePollEvery)
	err = g.finish(t.TempDir())
	_ = opened.Close()
	if err == nil {
		t.Fatal("a read-write open of the development database passed the guard")
	}
	for _, want := range []string{"held the development", "opened or modified during the suite", "jevons.db-wal exists false → true"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("guard error lacks %q: %v", want, err)
		}
	}
}

// While a development daemon owns the database, its writes are not the
// suite's: the fingerprint is not compared, and the scan still decides.
func TestT1082GuardSkipsTheFingerprintWhileADevelopmentDaemonRuns(t *testing.T) {
	home := t.TempDir()
	dev := filepath.Join(home, ".jevons")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statedb.DefaultPath(dev), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := startDevStateGuardWith(home, os.Getpid(), func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statedb.DefaultPath(dev), []byte("changed by the development daemon"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := g.finish(t.TempDir()); err != nil {
		t.Fatalf("development daemon's own write failed the guard: %v", err)
	}
	state := t.TempDir()
	seedToolRows(t, state, map[string][2]string{"jevons": {"tool_use", t1082Opener}})
	g, err = startDevStateGuardWith(home, os.Getpid(), func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err := g.finish(state); err == nil || !strings.Contains(err.Error(), "reached into the development state") {
		t.Fatalf("the opener passed while a development daemon ran: %v", err)
	}
}
