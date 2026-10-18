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

func TestIsolateControlFileIsTheBrokerControlFile(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	if err := linkIsolateControlFile(state, root); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(state, claudia.ClaudiaDBFile))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "state", claudia.ClaudiaDBFile)
	if got != want {
		t.Fatalf("control link %q, want %q", got, want)
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
