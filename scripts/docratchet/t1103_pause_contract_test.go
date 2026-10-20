// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestT1103PauseMarkerRefusesRestart is the executable oracle for 🎯T1103:
// an owner-authorised restart must not be raced by a host sentinel acting
// on a stale pause order. The restart script checks for the sentinel's
// pause marker before it does anything, refuses with the marker's own
// text plus the override that lifts it, and never touches the daemon.
//
// Motivating incident (docs/design/sentinel-pause-contract.md): on
// 2026-10-21 the owner approved a restart at 09:01, and at 09:05 a host
// sentinel holding a stale 2026-10-15 pause order stopped the freshly
// started daemon and reported the owner's own action as an incident.
func TestT1103PauseMarkerRefusesRestart(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts/restart-jevonsd.sh")

	bash := "/bin/bash"
	if _, err := os.Stat(bash); err != nil {
		bash = "bash"
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "jevons-paused.txt")
	markerText := "Paused by owner on 2026-10-15; do not start Jevons until explicit owner instruction"
	if err := os.WriteFile(marker, []byte(markerText+"\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	env := append(os.Environ(), "JEVONS_RESTART_PAUSE_MARKER="+marker)

	// --dry-run never touches the daemon on its own, which is what makes
	// it safe to drive from a hermetic test; the pause check runs before
	// even the dry-run plan is printed, so "refuses before activating
	// anything" holds trivially here and a fortiori for a real run.
	cmd := exec.Command(bash, script, "--dry-run")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("--dry-run with pause marker present: expected nonzero exit, got success\n%s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 3 {
		t.Errorf("expected exit 3 (refused), got %d\n%s", exitErr.ExitCode(), out)
	}
	got := string(out)
	for _, m := range []string{
		"refusing",
		marker,
		markerText, // the marker's own text, quoted verbatim
		"JEVONS_RESTART_OVERRIDE_PAUSE=1",
		"sentinel-pause-contract.md",
	} {
		if !strings.Contains(got, m) {
			t.Errorf("refusal output missing %q\nfull:\n%s", m, got)
		}
	}
	if strings.Contains(got, "[dry-run] would:") {
		t.Errorf("refusal must happen before the dry-run plan is printed, but found plan output\n%s", got)
	}

	// -h/--help is exempt: reading the manual is never blocked by a pause.
	helpCmd := exec.Command(bash, script, "--help")
	helpCmd.Env = env
	helpOut, err := helpCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--help with pause marker present: %v\n%s", err, helpOut)
	}
	if strings.Contains(string(helpOut), "refusing") {
		t.Errorf("--help should not be refused by the pause marker\n%s", helpOut)
	}

	// With the owner's explicit override, the same --dry-run proceeds
	// past the check and prints its plan.
	overrideCmd := exec.Command(bash, script, "--dry-run")
	overrideCmd.Env = append(env, "JEVONS_RESTART_OVERRIDE_PAUSE=1")
	overrideOut, err := overrideCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--dry-run with override: %v\n%s", err, overrideOut)
	}
	if !strings.Contains(string(overrideOut), "[dry-run] would:") {
		t.Errorf("override did not let --dry-run proceed to its plan\n%s", overrideOut)
	}
	if strings.Contains(string(overrideOut), "refusing") {
		t.Errorf("override run still printed a refusal\n%s", overrideOut)
	}
}

// TestT1103SupervisorInstallHonoursPause: supervisord autostart reaches the
// port without going through restart-jevonsd.sh at all, so the installer
// checks the same marker itself.
func TestT1103SupervisorInstallHonoursPause(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "supervisor/install.sh")

	dir := t.TempDir()
	marker := filepath.Join(dir, "jevons-paused.txt")
	markerText := "Paused by owner on 2026-10-15; do not start Jevons until explicit owner instruction"
	if err := os.WriteFile(marker, []byte(markerText+"\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// SUPERVISOR_SKIP_CTL short-circuits before supervisorctl is touched on
	// an unpaused run (see supervisor/install.sh), so the only thing this
	// test needs to prove is that the pause check runs, and runs first.
	confDir := t.TempDir()
	env := append(os.Environ(),
		"JEVONS_RESTART_PAUSE_MARKER="+marker,
		"SUPERVISOR_CONF_DIR="+confDir,
		"SUPERVISOR_SKIP_CTL=1",
	)

	cmd := exec.Command("sh", script)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("install.sh with pause marker present: expected nonzero exit, got success\n%s", out)
	}
	got := string(out)
	for _, m := range []string{"refusing", marker, markerText, "JEVONS_RESTART_OVERRIDE_PAUSE=1"} {
		if !strings.Contains(got, m) {
			t.Errorf("refusal output missing %q\nfull:\n%s", m, got)
		}
	}
	entries, _ := os.ReadDir(confDir)
	if len(entries) != 0 {
		t.Errorf("install.sh rendered into %s despite the pause marker: %v", confDir, entries)
	}

	overrideCmd := exec.Command("sh", script)
	overrideCmd.Env = append(env, "JEVONS_RESTART_OVERRIDE_PAUSE=1")
	overrideOut, err := overrideCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh with override: %v\n%s", err, overrideOut)
	}
	if strings.Contains(string(overrideOut), "refusing") {
		t.Errorf("override run still printed a refusal\n%s", overrideOut)
	}
	entries, _ = os.ReadDir(confDir)
	if len(entries) == 0 {
		t.Errorf("override did not let install.sh render into %s", confDir)
	}
}

// TestT1103DesignDocExists ratchets that the sentinel-side half of the
// contract is written down where the sentinel is told to read it.
func TestT1103DesignDocExists(t *testing.T) {
	body := readRepo(t, "docs/design/sentinel-pause-contract.md")
	for _, m := range []string{
		"🎯T1103",
		"jevons-paused.txt",
		"JEVONS_RESTART_OVERRIDE_PAUSE",
		"2026-10-21",
		"🎯T1086",
		"🎯T405",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("sentinel-pause-contract.md missing marker %q", m)
		}
	}
}
