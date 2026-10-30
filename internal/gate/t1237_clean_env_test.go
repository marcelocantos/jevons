// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import "testing"

// CleanArgs.Env reaches the command (🎯T1237): the daemon's standing gate
// relies on it for CLAUDIA_NO_BROKER, which `make test-go` would export.
func TestT1237CleanEnvReachesTheCommand(t *testing.T) {
	root, _ := fixtureRepo(t, true)
	store := storeAt(t)
	check := []string{"sh", "-c", `test "$T1237_PROBE" = set`}

	res, err := RunClean(&CleanArgs{Command: check, Repo: root, WorkDir: t.TempDir(), Store: store})
	if err != nil {
		t.Fatalf("clean run without env: %v", err)
	}
	if res.Record.Verdict.IsGreen() {
		t.Fatalf("probe passed without Env: %s", res.Record.Verdict)
	}
	res, err = RunClean(&CleanArgs{Command: check, Repo: root, WorkDir: t.TempDir(), Store: store, Env: []string{"T1237_PROBE=set"}})
	if err != nil {
		t.Fatalf("clean run with env: %v", err)
	}
	if res.Record.ExitStatus != 0 {
		t.Fatalf("probe with Env: verdict %s exit %d", res.Record.Verdict, res.Record.ExitStatus)
	}
}
