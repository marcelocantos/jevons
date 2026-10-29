// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/marcelocantos/jevons/internal/supervise"
)

func supervisorPlistPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "supervisor", "homebrew.mxcl.supervisor.plist")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("supervisor launchagent template: %v", err)
	}
	return path
}

func TestT779SupervisorLaunchAgentIsInteractive(t *testing.T) {
	body, err := os.ReadFile(supervisorPlistPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !supervise.InteractiveSupervisorLaunchAgentPlist(body) {
		t.Fatal("homebrew.mxcl.supervisor.plist must declare ProcessType Interactive for supervisord (🎯T779)")
	}
}
