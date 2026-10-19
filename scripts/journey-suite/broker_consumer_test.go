// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestIsolatedBrokerCommandCarriesConsumerConfiguration(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	bin, sidecar := filepath.Join(root, "claudia"), filepath.Join(root, "server.ts")
	cmd := isolatedBrokerCommand([]string{
		"JEVONS_SPOOL_DIR=/owner/legacy", "CLAUDIA_OMP_SPOOL_DIR=/owner/neutral",
		"CLAUDIA_OMP_SPOOL_DIR=", "XDG_STATE_HOME=/owner/state",
	}, bin, root, state, sidecar)
	wantArgs := []string{bin, "broker", "serve", "-state-dir", filepath.Join(root, "state"),
		"-socket", filepath.Join(root, "broker.sock"), "-no-resume", "-restart-nudge", "-",
		"-mcp-seed-dir", state, "-mcp-consumer-prefix", "jevonsmcp"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("broker invocation=%v, want %v", cmd.Args, wantArgs)
	}
	for name, want := range map[string]string{
		"JEVONS_SPOOL_DIR":      filepath.Join(state, "spool"),
		"CLAUDIA_OMP_SPOOL_DIR": filepath.Join(state, "spool"),
		"XDG_STATE_HOME":        root, "CLAUDIA_OMP_SERVER": sidecar,
		"CLAUDIA_OMP_NO_REFRESH": "1",
	} {
		count := 0
		for _, entry := range cmd.Env {
			if key, _, _ := strings.Cut(entry, "="); key == name {
				count++
			}
		}
		if got := findEnv(cmd.Env, name); got != want || count != 1 {
			t.Fatalf("%s=%q count=%d, want %q exactly once", name, got, count, want)
		}
	}
}

// The broker's control file lives in the broker's state directory and the
// journey only names it there. Nothing in the jevons state carries the
// broker's file name: the isolate daemon keeps its own control-replica.db
// and links to the broker over the control socket (🎯T1074.9).
func TestIsolateControlFileBelongsToTheBroker(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	b := &isolatedBroker{root: root}
	want := filepath.Join(root, "state", claudia.ClaudiaDBFile)
	if got := b.controlFile(); got != want {
		t.Fatalf("broker control file %q, want %q", got, want)
	}
	cmd := isolatedBrokerCommand(nil, "claudia", root, state, filepath.Join(root, "server.ts"))
	stateDir := ""
	for i, arg := range cmd.Args {
		if arg == "-state-dir" && i+1 < len(cmd.Args) {
			stateDir = cmd.Args[i+1]
		}
	}
	if stateDir != filepath.Dir(want) {
		t.Fatalf("broker state %q, want the control file's directory %q", stateDir, filepath.Dir(want))
	}
	if stateDir == state || strings.HasSuffix(stateDir, "/.jevons") {
		t.Fatalf("broker grants landed in the jevons state %q", stateDir)
	}
	if _, err := os.Lstat(filepath.Join(state, claudia.ClaudiaDBFile)); !os.IsNotExist(err) {
		t.Fatalf("jevons state carries the broker's file name: %v", err)
	}
}

// openingPIDs is J44's topology probe. It names this process while the file
// is open here, and no process once it is closed.
func TestOpeningPIDsNamesTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held.db")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	pids, err := openingPIDs(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pids) != 1 || pids[0] != os.Getpid() {
		t.Fatalf("holders of an open file = %v, want [%d]", pids, os.Getpid())
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	pids, err = openingPIDs(path)
	if err != nil || len(pids) != 0 {
		t.Fatalf("holders of a closed file = %v %v", pids, err)
	}
}

func TestSnapshotControlFileCopiesWALSiblings(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	db := []byte("control-bytes")
	wal := []byte("wal-bytes")
	if err := os.WriteFile(filepath.Join(state, claudia.ClaudiaDBFile), db, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, claudia.ClaudiaDBFile+"-wal"), wal, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "grants.db"), []byte("grants"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "control-snapshot")
	if err := snapshotControlFile(root, dest); err != nil {
		t.Fatal(err)
	}
	gotDB, err := os.ReadFile(filepath.Join(dest, claudia.ClaudiaDBFile))
	if err != nil {
		t.Fatal(err)
	}
	gotWAL, err := os.ReadFile(filepath.Join(dest, claudia.ClaudiaDBFile+"-wal"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotDB) != string(db) || string(gotWAL) != string(wal) {
		t.Fatalf("snapshot db=%q wal=%q", gotDB, gotWAL)
	}
	if _, err := os.Stat(filepath.Join(dest, "grants.db")); !os.IsNotExist(err) {
		t.Fatalf("grant file copied: %v", err)
	}
}

func TestSnapshotControlFileMissingIsQuiet(t *testing.T) {
	if err := snapshotControlFile(t.TempDir(), filepath.Join(t.TempDir(), "control-snapshot")); err != nil {
		t.Fatal(err)
	}
}

func TestDumpIsolateLogsIncludesBroker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claudia-broker.log"), []byte("broker-saw-the-seat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	dumpIsolateLogs(&buf, dir, 20)
	if !strings.Contains(buf.String(), "broker-saw-the-seat") {
		t.Fatalf("broker log missing from %q", buf.String())
	}
}
