// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
