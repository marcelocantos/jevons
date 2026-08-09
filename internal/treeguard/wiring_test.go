package treeguard_test

// 🎯T376 wiring ratchet. An unwired guard is not a guard: the policy can be
// perfect and the fleet still clobbers itself if .claude/settings.json stops
// calling the hook, the shim loses its execute bit, or `make all` stops
// building the binary the shim execs. Each of those is a silent failure, which
// is exactly the class this target exists to eliminate — so each is asserted.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	settingsPath = ".claude/settings.json"
	shimPath     = "scripts/hooks/treeguard"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGuardHookIsWiredInProjectSettings(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, settingsPath))
	if err != nil {
		t.Fatalf("%s missing — the write guard is not wired: %v", settingsPath, err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("%s is not valid JSON: %v", settingsPath, err)
	}

	// Pre must cover every tool that can replace file content; Post must also
	// cover Read, because Read is what establishes a session's base.
	wantPre := []string{"Write", "Edit", "MultiEdit"}
	wantPost := append([]string{"Read"}, wantPre...)
	for event, wantTools := range map[string][]string{
		"PreToolUse":  wantPre,
		"PostToolUse": wantPost,
	} {
		entries, ok := settings.Hooks[event]
		if !ok {
			t.Errorf("%s has no %s hook", settingsPath, event)
			continue
		}
		var matchers, commands []string
		for _, e := range entries {
			for _, h := range e.Hooks {
				if strings.Contains(h.Command, "treeguard") {
					matchers = append(matchers, e.Matcher)
					commands = append(commands, h.Command)
				}
			}
		}
		if len(commands) == 0 {
			t.Errorf("%s %s does not invoke treeguard", settingsPath, event)
			continue
		}
		joined := strings.Join(matchers, "|")
		for _, tool := range wantTools {
			if !strings.Contains(joined, tool) {
				t.Errorf("%s %s matcher %q does not cover %s", settingsPath, event, joined, tool)
			}
		}
		want := "pre"
		if event == "PostToolUse" {
			want = "post"
		}
		if !strings.Contains(strings.Join(commands, " "), "treeguard "+want) {
			t.Errorf("%s %s invokes treeguard without the %q mode: %v", settingsPath, event, want, commands)
		}
	}
}

func TestGuardArtifactsAreTrackedAndExecutable(t *testing.T) {
	root := repoRoot(t)

	// Both files are inside a directory the repo otherwise ignores or that a
	// clean clone must carry; a untracked guard reaches nobody else's worker.
	for _, rel := range []string{settingsPath, shimPath} {
		cmd := exec.Command("git", "ls-files", "--error-unmatch", rel)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s is not tracked by git, so other workers never get it: %v\n%s", rel, err, out)
		}
	}

	info, err := os.Stat(filepath.Join(root, shimPath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable; the hook cannot run it", shimPath)
	}
}

func TestMakeAllBuildsTheGuardBinary(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(raw)
	if !strings.Contains(makefile, "bin/treeguard: ") {
		t.Error("Makefile has no bin/treeguard rule; the hook shim would find no binary")
	}
	for line := range strings.SplitSeq(makefile, "\n") {
		if strings.HasPrefix(line, "all:") {
			if !strings.Contains(line, "treeguard") {
				t.Errorf("`make all` does not build the guard: %q", line)
			}
			return
		}
	}
	t.Error("Makefile has no all: target")
}
