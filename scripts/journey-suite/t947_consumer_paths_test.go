// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T947: every jevons-controlled consumer of the shared plan token must refuse
// to rotate it. The development broker (not started here) is the sole refresher;
// these paths only read or proxy credentials.
func TestT947JevonsControlledConsumersNeverRefresh(t *testing.T) {
	t.Run("isolated broker", func(t *testing.T) {
		env := isolatedBrokerEnv(nil, "/tmp/a/broker.sock", "/tmp/a/omp.sock", "/tmp/a/server.ts",
			"/tmp/a/state", "/tmp/a/spool")
		if findEnv(env, "CLAUDIA_OMP_NO_REFRESH") != "1" {
			t.Fatalf("isolated broker must set CLAUDIA_OMP_NO_REFRESH=1")
		}
	})
	t.Run("isolate daemon", func(t *testing.T) {
		t.Setenv("CLAUDIA_OMP_NO_REFRESH", "")
		s := &suite{stateDir: t.TempDir()}
		if findEnv(s.isolateDaemonEnv(), "CLAUDIA_OMP_NO_REFRESH") != "1" {
			t.Fatal("isolate daemon must force CLAUDIA_OMP_NO_REFRESH=1")
		}
	})
	t.Run("isolated broker command", func(t *testing.T) {
		root, state := t.TempDir(), t.TempDir()
		cmd := isolatedBrokerCommand(nil, filepath.Join(root, "claudia"), root, state, filepath.Join(root, "server.ts"))
		if findEnv(cmd.Env, "CLAUDIA_OMP_NO_REFRESH") != "1" {
			t.Fatal("isolatedBrokerCommand must carry CLAUDIA_OMP_NO_REFRESH=1")
		}
		if !strings.Contains(strings.Join(cmd.Args, " "), "-mcp-consumer-prefix") {
			t.Fatal("isolated broker must name its MCP consumer prefix")
		}
	})
}
