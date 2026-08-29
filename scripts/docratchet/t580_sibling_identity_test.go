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

// 🎯T580 — THE RESTART SEES A SIBLING-ONLY CHANGE.
//
// On 2026-08-29 a claudia-only fix (f8869fe) was rebuilt into bin/jevonsd and
// the restart still logged "🎯T218 already activated: :13705 serves this exact
// build (sha be40be43bc66)", leaving the pre-fix daemon serving until someone
// passed --force. jevons consumes local-master claudia through ../go.work
// (🎯T448), so a sibling commit changes what is built while this repo's tree —
// and the compiled binary's hash, that day — reported nothing moved.
//
// The fixture reproduces exactly that shape: the binary on disk is byte-for-byte
// the one already serving, and the ONLY thing that moved is the sibling's HEAD.
// Before the fix this run short-circuits; after it, it bounces.
func TestT580SiblingHEADForcesActivation(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable; the oracle needs it to read the listening pid")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable for the fake daemon")
	}

	e := newThrashEnv(t)
	sibling := makeWorkspace(t, e.root)
	e.build("a")

	out, err := e.run(0)
	if err != nil {
		t.Fatalf("cold start failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "🎯T580 source identity:") {
		t.Fatalf("the restart did not compute a source identity:\n%s", out)
	}
	first := e.listenerPID()
	if first == 0 {
		t.Fatalf("nothing listening on :%d after cold start:\n%s", e.port, out)
	}

	// Control: nothing moved at all, so the thrash policy must still hold.
	// Without this, a test that only proves "it restarts" would also pass
	// for an identity that changes on every run.
	out, err = e.run(0)
	if err != nil {
		t.Fatalf("no-op run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "already activated") {
		t.Fatalf("an unchanged tree did not short-circuit; the identity is not stable:\n%s", out)
	}
	if got := e.listenerPID(); got != first {
		t.Fatalf("unchanged tree bounced the daemon %d → %d", first, got)
	}

	// THE ORACLE: move the sibling's HEAD and nothing else. bin/jevonsd is
	// not rebuilt, so its hash is identical to what is serving — the exact
	// state the old check called "this exact build".
	gitIn(t, sibling, "commit", "--allow-empty", "-m", "sibling-only readiness fix")

	out, err = e.run(0)
	if err != nil {
		t.Fatalf("sibling-moved run failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "already activated") {
		t.Fatalf("a sibling-only change was treated as already activated (🎯T580):\n%s", out)
	}
	if !strings.Contains(out, "same binary hash, different sources") {
		t.Fatalf("the restart did not name the sibling difference:\n%s", out)
	}
	if got := e.listenerPID(); got == first || got == 0 {
		t.Fatalf("the daemon was not activated for the sibling change: pid %d → %d\n%s", first, got, out)
	}
}

// makeWorkspace turns the fixture root into a git checkout inside a go.work
// workspace with a claudia sibling beside it — the layout the real clone has
// (…/marcelocantos/{go.work,jevons,claudia}). Returns the sibling path.
func makeWorkspace(t *testing.T, root string) string {
	t.Helper()
	base := filepath.Dir(root)
	sibling := filepath.Join(base, "claudia")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "go.mod"),
		[]byte("module github.com/marcelocantos/claudia\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, sibling} {
		gitIn(t, dir, "init")
		gitIn(t, dir, "config", "user.email", "t580@test")
		gitIn(t, dir, "config", "user.name", "t580")
		gitIn(t, dir, "commit", "--allow-empty", "-m", "base")
	}
	work := "go 1.26.1\n\nuse (\n\t./" + filepath.Base(root) + "\n\t./claudia\n)\n"
	if err := os.WriteFile(filepath.Join(base, "go.work"), []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}
	return sibling
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}
