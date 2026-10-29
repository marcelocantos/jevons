// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T598: workspace-write alone gives a Codex seat an empty writable-root
// list and no network — enough to edit files, not enough to prove
// anything. bin/gate records under ~/.jevons/gates, outside the tree on
// purpose (🎯T386), and the journey oracles bind loopback.
func TestCodexWorkSeatCanReachTheGateStoreAndLoopback(t *testing.T) {
	roots, network := CodexWorkSandboxTuning(claudia.ProviderCodex, claudia.PurposeWork, "")
	if !network {
		t.Fatal("a work seat that cannot bind loopback cannot run a journey")
	}
	want := gate.DefaultStoreRoot()
	if want == "" {
		t.Fatal("gate store root is empty; the fixture cannot mean anything")
	}
	found := false
	for _, r := range roots {
		if r == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("gate store %q not writable; roots=%v", want, roots)
	}
}

// The tuning follows the mode: seats that get no Codex sandbox get no
// widening either. Otherwise this would quietly grant network access to
// every provider.
func TestOnlyCodexWorkSeatsAreWidened(t *testing.T) {
	cases := []struct {
		name    string
		prov    claudia.Provider
		purpose string
		role    string
	}{
		{"claude work", claudia.ProviderClaude, claudia.PurposeWork, ""},
		{"grok work", claudia.ProviderGrok, claudia.PurposeWork, ""},
		{"codex overseer", claudia.ProviderCodex, claudia.PurposeOverseer, ""},
		{"codex aside", claudia.ProviderCodex, claudia.PurposeAside, ""},
		{"codex auditor", claudia.ProviderCodex, claudia.PurposeWork, "auditor"},
	}
	for _, c := range cases {
		roots, network := CodexWorkSandboxTuning(c.prov, c.purpose, c.role)
		if len(roots) != 0 || network {
			t.Fatalf("%s was widened: roots=%v network=%v", c.name, roots, network)
		}
	}
}

// The widening rides the same decision as the mode, so a mint path cannot
// set one without the other.
func TestTuningTracksTheSandboxMode(t *testing.T) {
	for _, c := range []struct {
		prov    claudia.Provider
		purpose string
		role    string
	}{
		{claudia.ProviderCodex, claudia.PurposeWork, ""},
		{claudia.ProviderCodex, claudia.PurposeWork, "auditor"},
		{claudia.ProviderClaude, claudia.PurposeWork, ""},
		{claudia.ProviderCodex, claudia.PurposeAside, ""},
	} {
		mode := CodexWorkSandbox(c.prov, c.purpose, c.role)
		roots, network := CodexWorkSandboxTuning(c.prov, c.purpose, c.role)
		widened := len(roots) > 0 || network
		if (mode != "") != widened {
			t.Fatalf("mode=%q but widened=%v for %v/%s/%s", mode, widened, c.prov, c.purpose, c.role)
		}
	}
}

// A seat that cannot get what its mission needs is refused with a reason,
// rather than started into a first gate it can never pass — the 🎯T557.1
// shape, where the seat looked healthy and could produce no evidence.
func TestRefusalNamesTheMissingAccess(t *testing.T) {
	if why := CodexWorkSandboxRefusal(claudia.ProviderCodex, claudia.PurposeWork, ""); why != "" {
		t.Fatalf("a healthy work seat was refused: %s", why)
	}
	// Seats that were never widened are not refused either — they simply
	// are not Codex work seats.
	for _, c := range []struct {
		prov    claudia.Provider
		purpose string
	}{
		{claudia.ProviderClaude, claudia.PurposeWork},
		{claudia.ProviderCodex, claudia.PurposeOverseer},
	} {
		if why := CodexWorkSandboxRefusal(c.prov, c.purpose, ""); why != "" {
			t.Fatalf("%v/%s refused: %s", c.prov, c.purpose, why)
		}
	}
}

// The refusal must say what is missing and how it bites, or it is just
// another opaque stall.
func TestRefusalTextIsActionable(t *testing.T) {
	t.Setenv(gate.StoreDirEnv, " ")
	why := CodexWorkSandboxRefusal(claudia.ProviderCodex, claudia.PurposeWork, "")
	if why == "" {
		t.Skip("gate store root still resolves; nothing to refuse")
	}
	if !strings.Contains(why, "gate") {
		t.Fatalf("refusal does not name the missing access: %q", why)
	}
}

// 🎯T1232: a Codex work seat must be able to compile and test, not only edit.
// jv-t1228-controlplane-retry could not open the Go build cache or create the
// testhome root under workspace-write (2026-10-27).
func TestCodexWorkSeatCanReachBuildAndTestCaches(t *testing.T) {
	t.Setenv("GOCACHE", "")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", "")
	roots, _ := CodexWorkSandboxTuning(claudia.ProviderCodex, claudia.PurposeWork, "")
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip("no user cache dir on this host")
	}
	home, _ := os.UserHomeDir()
	for _, want := range []string{
		filepath.Join(cache, "go-build"),
		filepath.Join(home, "go", "pkg", "mod"),
		filepath.Join(cache, "jevons-testhome"),
		filepath.Join(cache, "ms-playwright"),
	} {
		if !slices.Contains(roots, want) {
			t.Errorf("Codex work grant lacks %q; roots=%v", want, roots)
		}
	}
	t.Setenv("GOCACHE", "/custom/gocache")
	roots, _ = CodexWorkSandboxTuning(claudia.ProviderCodex, claudia.PurposeWork, "")
	if !slices.Contains(roots, "/custom/gocache") {
		t.Errorf("GOCACHE override not granted; roots=%v", roots)
	}
}

// 🎯T1232: integrate, the only landing path for a worktree seat, runs as the
// seat's child and writes the shared clone. A Codex work seat's grant names
// that clone, so jv-t1228-retry-2's GREEN work could land from its own seat.
func TestCodexWorktreeSeatGrantIncludesItsSharedClone(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "proj")
	seat := filepath.Join(root, ".jevons-worktrees-proj", "jv-seat")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(repo, "init", "-q")
	git(repo, "commit", "-q", "--allow-empty", "-m", "base")
	//worktreereap:exempt the repo and its worktree both live in t.TempDir; no shared clone or host tree is touched
	git(repo, "worktree", "add", "-q", seat)

	def := &claudia.AgentDef{Name: "jv-seat", Provider: claudia.ProviderCodex, Purpose: claudia.PurposeWork, WorkDir: seat}
	ApplyWorkSeatSandbox(def, "")
	want, _ := filepath.EvalSymlinks(repo)
	found := false
	for _, r := range def.SandboxWritableRoots {
		if got, _ := filepath.EvalSymlinks(r); got == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("shared clone %q missing from Codex grant %v", want, def.SandboxWritableRoots)
	}

	plain := &claudia.AgentDef{Name: "jv-plain", Provider: claudia.ProviderCodex, Purpose: claudia.PurposeWork, WorkDir: repo}
	ApplyWorkSeatSandbox(plain, "")
	if len(plain.SandboxWritableRoots) != len(def.SandboxWritableRoots)-1 {
		t.Fatalf("a seat that is not a worktree gained an extra root: %v", plain.SandboxWritableRoots)
	}
}
