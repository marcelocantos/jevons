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

// TestRestartDailyJevonsdScript is the hermetic oracle for 🎯T191:
// committed restart script, help/dry-run, detach markers (nohup/setsid),
// bash 3.2 syntax under /bin/bash -n. Does not bounce the daily daemon.
func TestRestartDailyJevonsdScript(t *testing.T) {
	root := repoRoot(t)
	rel := "scripts/restart-daily-jevonsd.sh"
	path := filepath.Join(root, rel)

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("script missing: %v", err)
	}
	if st.Mode()&0o111 == 0 {
		t.Fatalf("%s is not executable (mode %v)", rel, st.Mode())
	}

	body := readRepo(t, rel)
	for _, m := range []string{
		"BLESSED INVOKE",
		"nohup",
		"setsid",
		"brew services stop",
		"bin/jevonsd",
		"/health",
		"/api/frontier",
		"--dry-run",
		"--help",
		"make",
		"WORKDIR",
		"survive",
		"fleet agent",
		"🎯T191",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("%s missing marker %q", rel, m)
		}
	}

	// macOS ships bash 3.2 at /bin/bash — syntax-check there, not Homebrew 5.x.
	bash := "/bin/bash"
	if _, err := os.Stat(bash); err != nil {
		bash = "bash"
	}
	out, err := exec.Command(bash, "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("%s -n %s failed: %v\n%s", bash, rel, err, out)
	}

	helpOut, err := exec.Command(bash, path, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("--help failed: %v\n%s", err, helpOut)
	}
	help := string(helpOut)
	for _, m := range []string{"BLESSED INVOKE", "nohup", "--dry-run"} {
		if !strings.Contains(help, m) {
			t.Errorf("--help missing %q", m)
		}
	}

	dryOut, err := exec.Command(bash, path, "--dry-run").CombinedOutput()
	if err != nil {
		t.Fatalf("--dry-run failed: %v\n%s", err, dryOut)
	}
	dry := string(dryOut)
	for _, m := range []string{"[dry-run]", "bin/jevonsd", "brew services stop", "/api/frontier", "BLESSED INVOKE"} {
		if !strings.Contains(dry, m) {
			t.Errorf("--dry-run output missing %q\nfull:\n%s", m, dry)
		}
	}
}

// TestRestartDailyDoctrineMarkers ratchets 🎯T188 + 🎯T191 prose: after
// daemon-path Build, invoke the restart script detached; owner never restarts
// by hand; do not claim fixed until script success.
func TestRestartDailyDoctrineMarkers(t *testing.T) {
	persona := readRepo(t, "internal/config/persona.md")
	agents := readRepo(t, "AGENTS.md")
	guide := readRepo(t, "agents-guide.md")

	for _, doc := range []struct {
		name, body string
		need       []string
	}{
		{"internal/config/persona.md", persona, []string{
			"🎯T188",
			"🎯T191",
			"restart-daily-jevonsd.sh",
			"nohup",
			"owner never restarts",
			"daemon-path",
		}},
		{"AGENTS.md", agents, []string{
			"🎯T188",
			"🎯T191",
			"restart-daily-jevonsd.sh",
			"nohup",
		}},
		{"agents-guide.md", guide, []string{
			"🎯T191",
			"restart-daily-jevonsd.sh",
			"nohup",
			"BLESSED INVOKE",
		}},
	} {
		for _, m := range doc.need {
			if !strings.Contains(doc.body, m) {
				t.Errorf("%s missing doctrine marker %q", doc.name, m)
			}
		}
	}
}
