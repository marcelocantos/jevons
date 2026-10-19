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

// 🎯T700 — an unattended supervision pass can reach the owner on the one
// channel it is told to escalate on, without an interactive permission
// grant. The runner (run-tick.sh) must bypass permission prompts; the
// one-shot wrapper must treat a refused blurter as a loud failure, not quiet.

func TestT700RunTickBypassesPermissionPromptsForEscalation(t *testing.T) {
	body := readRepo(t, "supervisor/run-tick.sh")
	for _, m := range []string{
		"🎯T700",
		"--permission-mode",
		"bypassPermissions",
		"--add-dir",
		"$HOME/.jevons",
		"blurter",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("supervisor/run-tick.sh missing 🎯T700 marker %q", m)
		}
	}
}

func TestT700BriefNamesBlurterCLIAsEscalationPath(t *testing.T) {
	// The production brief lives under ~/.jevons/supervisor (operator state).
	// The tracked brief template is what install / operator seeds from; if it
	// is missing, the live brief still must name the CLI path. We ratchet the
	// runner comments and the one-shot loud-fail instead of the live brief.
	tick := readRepo(t, "supervisor/jevons-tick")
	for _, m := range []string{
		"🎯T700",
		"muzzled",
		"requires approval",
		"blurter",
		"not quiet",
	} {
		if !strings.Contains(tick, m) {
			t.Errorf("supervisor/jevons-tick missing 🎯T700 loud-fail marker %q", m)
		}
	}
}

func TestT700MuzzledTranscriptIsNotQuiet(t *testing.T) {
	// Hermetic: a fake claude that prints the 2026-09-20 refusal shape must
	// make jevons-tick exit non-zero with a last-line that is not "quiet".
	root := repoRoot(t)
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	brief := filepath.Join(state, "brief.md")
	if err := os.WriteFile(brief, []byte("supervise\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeClaude := filepath.Join(dir, "claude")
	script := "#!/bin/sh\necho 'blurter send --app jevons-supervisor'\necho 'This command requires approval'\necho quiet\n"
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(filepath.Join(root, "supervisor/jevons-tick"))
	cmd.Env = append(os.Environ(),
		"JEVONS_SUPERVISOR_STATE="+state,
		"JEVONS_TICK_BRIEF="+brief,
		"JEVONS_CLAUDE_BIN="+fakeClaude,
		"JEVONS_TICK_REPO="+root,
		"JEVONS_TICK_TIMEOUT=5",
		"PATH="+dir+":/usr/bin:/bin",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit on muzzled blurter; out=%s", out)
	}
	last, err := os.ReadFile(filepath.Join(state, "last-line"))
	if err != nil {
		t.Fatalf("last-line: %v (out=%s)", err, out)
	}
	line := strings.TrimSpace(string(last))
	if line == "quiet" || line == "" {
		t.Fatalf("muzzled pass last-line=%q — must not look quiet", line)
	}
	if !strings.Contains(line, "muzzled") && !strings.Contains(line, "T700") {
		t.Fatalf("last-line=%q does not name the muzzled/T700 failure", line)
	}
}

func TestT700CleanEscalationPassIsQuietOk(t *testing.T) {
	// Control: a pass that escalates successfully may end with any non-muzzled line.
	root := repoRoot(t)
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	brief := filepath.Join(state, "brief.md")
	if err := os.WriteFile(brief, []byte("supervise\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeClaude := filepath.Join(dir, "claude")
	// Simulate a successful unattended escalation (blurter CLI exit 0).
	script := "#!/bin/sh\necho 'ran blurter send ok'\necho 'escalated: fixture wake-worthy'\nexit 0\n"
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(root, "supervisor/jevons-tick"))
	cmd.Env = append(os.Environ(),
		"JEVONS_SUPERVISOR_STATE="+state,
		"JEVONS_TICK_BRIEF="+brief,
		"JEVONS_CLAUDE_BIN="+fakeClaude,
		"JEVONS_TICK_REPO="+root,
		"JEVONS_TICK_TIMEOUT=5",
		"PATH="+dir+":/bin:/usr/bin",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clean pass should succeed: %v out=%s", err, out)
	}
	last, _ := os.ReadFile(filepath.Join(state, "last-line"))
	if strings.Contains(string(last), "muzzled") {
		t.Fatalf("clean pass marked muzzled: %s", last)
	}
}
