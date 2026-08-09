package treeguard_test

// 🎯T376 acceptance oracle: two workers editing disjoint regions of a shared
// hot file in the same window must both survive to HEAD.
//
// The pair of tests here is the mutation evidence. They run the identical
// interleaving:
//
//	TestConcurrentDisjointEditsBothSurviveWithGuard — guard on: worker B's
//	stale write is refused, B re-reads, both regions reach HEAD.
//	TestConcurrentDisjointEditsLoseRegionWithoutGuard — guard off: B's stale
//	write is accepted and worker A's region is GONE from HEAD.
//
// The second test is the defect, asserted as a fact rather than described in a
// comment: it fails the moment the clobber stops being reproducible, and it
// proves the first test is not green for some unrelated reason.
//
// To re-run the first test against a guard that does not refuse (the pre-fix
// tree), point TREEGUARD_BIN at an always-allow binary — see the package doc
// note on BinEnv in this file.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// BinEnv points the oracle at an alternate guard binary instead of building
// cmd/treeguard. Used to demonstrate the oracle red against a guard that never
// refuses.
const BinEnv = "TREEGUARD_BIN"

const (
	regionAMarker = "<!-- REGION-A -->"
	regionBMarker = "<!-- REGION-B -->"
	// The real 🎯T370 loss: a script tag and a title, disjoint regions of
	// web/index.html, each written by a different fleet worker.
	regionAEdit = regionAMarker + "\n<script src=\"scripts/fleet_cycle.js\"></script>"
	regionBEdit = regionBMarker + "\n<div class=\"sidebar-composer-title\">Composer</div>"

	fixtureName = "index.html"
	fixture     = `<html>
<body>
<div id="top">base content</div>
` + regionAMarker + `
` + regionBMarker + `
</body>
</html>
`
)

func TestConcurrentDisjointEditsBothSurviveWithGuard(t *testing.T) {
	repo, bin := newScenario(t)
	a := &worker{t: t, bin: bin, repo: repo, session: "worker-a", guard: true}
	b := &worker{t: t, bin: bin, repo: repo, session: "worker-b", guard: true}

	// Both workers read the same base: this is the window in which the loss
	// happens.
	baseA := a.read(fixtureName)
	baseB := b.read(fixtureName)

	a.mustWrite(fixtureName, replaceOnce(t, baseA, regionAMarker, regionAEdit))
	a.commit(fixtureName, "worker A: fleet_cycle script tag")

	// B still holds the pre-A content. Its full-file write would revert A.
	allowed, stderr := b.write(fixtureName, replaceOnce(t, baseB, regionBMarker, regionBEdit))
	if allowed {
		t.Fatal("guard allowed a stale full-file write; worker A's edit was clobberable")
	}
	if !strings.Contains(stderr, "fleet_cycle.js") {
		t.Errorf("refusal did not name the at-risk line, so the loss is still hard to diagnose:\n%s", stderr)
	}
	if !strings.Contains(stderr, "T376") {
		t.Errorf("refusal did not cite the target:\n%s", stderr)
	}

	// The refusal is recoverable: re-read, re-apply, write.
	freshB := b.read(fixtureName)
	b.mustWrite(fixtureName, replaceOnce(t, freshB, regionBMarker, regionBEdit))
	b.commit(fixtureName, "worker B: sidebar composer title")

	head := gitShowHead(t, repo, fixtureName)
	if !strings.Contains(head, "fleet_cycle.js") {
		t.Error("worker A's edit is absent at HEAD")
	}
	if !strings.Contains(head, "sidebar-composer-title") {
		t.Error("worker B's edit is absent at HEAD")
	}
}

func TestConcurrentDisjointEditsLoseRegionWithoutGuard(t *testing.T) {
	repo, bin := newScenario(t)
	a := &worker{t: t, bin: bin, repo: repo, session: "worker-a", guard: false}
	b := &worker{t: t, bin: bin, repo: repo, session: "worker-b", guard: false}

	baseA := a.read(fixtureName)
	baseB := b.read(fixtureName)

	a.mustWrite(fixtureName, replaceOnce(t, baseA, regionAMarker, regionAEdit))
	a.commit(fixtureName, "worker A: fleet_cycle script tag")

	if allowed, _ := b.write(fixtureName, replaceOnce(t, baseB, regionBMarker, regionBEdit)); !allowed {
		t.Fatal("guard refused with JEVONS_TREEGUARD=off; the control proves nothing")
	}
	b.commit(fixtureName, "worker B: sidebar composer title")

	head := gitShowHead(t, repo, fixtureName)
	if strings.Contains(head, "fleet_cycle.js") {
		t.Error("unguarded stale write did NOT clobber worker A — the scenario no longer reproduces the defect, so the guarded test proves nothing")
	}
	if !strings.Contains(head, "sidebar-composer-title") {
		t.Error("worker B's own edit is missing; the control scenario is broken")
	}
}

// newScenario returns a fresh git repo containing the fixture, plus the guard
// binary to drive.
func newScenario(t *testing.T) (repo, bin string) {
	t.Helper()
	bin = guardBinary(t)
	repo = t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, fixtureName), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "master")
	git(t, repo, "add", fixtureName)
	git(t, repo, "commit", "-m", "base")
	return repo, bin
}

func guardBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv(BinEnv); bin != "" {
		return bin
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "treeguard")
	build := exec.Command("go", "build", "-o", bin, "./cmd/treeguard")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/treeguard: %v\n%s", err, out)
	}
	return bin
}

// worker drives the guard the way a real Claude Code fleet worker does: a Read
// records the base, a Write asks the PreToolUse hook first.
type worker struct {
	t       *testing.T
	bin     string
	repo    string
	session string
	guard   bool

	storeOnce string
}

// store is per-worker on disk, mirroring the per-session observation layout.
func (w *worker) store() string {
	if w.storeOnce == "" {
		w.storeOnce = filepath.Join(w.repo, ".treeguard-store")
	}
	return w.storeOnce
}

func (w *worker) read(rel string) []byte {
	w.t.Helper()
	content, err := os.ReadFile(filepath.Join(w.repo, rel))
	if err != nil {
		w.t.Fatal(err)
	}
	if exit, stderr := w.hook("post", "Read", rel, nil); exit != 0 {
		w.t.Fatalf("post-read hook exit %d: %s", exit, stderr)
	}
	return content
}

// write runs the guard, then performs the write only if it was allowed.
func (w *worker) write(rel string, content []byte) (allowed bool, stderr string) {
	w.t.Helper()
	body := string(content)
	exit, out := w.hook("pre", "Write", rel, &body)
	switch exit {
	case 0:
	case 2:
		return false, out
	default:
		w.t.Fatalf("pre-write hook failed: exit %d: %s", exit, out)
	}
	if err := os.WriteFile(filepath.Join(w.repo, rel), content, 0o644); err != nil {
		w.t.Fatal(err)
	}
	if exit, out := w.hook("post", "Write", rel, &body); exit != 0 {
		w.t.Fatalf("post-write hook exit %d: %s", exit, out)
	}
	return true, ""
}

func (w *worker) mustWrite(rel string, content []byte) {
	w.t.Helper()
	if allowed, stderr := w.write(rel, content); !allowed {
		w.t.Fatalf("guard refused a write it should have allowed: %s", stderr)
	}
}

func (w *worker) commit(rel, msg string) {
	w.t.Helper()
	git(w.t, w.repo, "add", rel)
	git(w.t, w.repo, "commit", "-m", msg, "--", rel)
}

func (w *worker) hook(mode, tool, rel string, content *string) (exit int, stderr string) {
	w.t.Helper()
	input := map[string]any{"file_path": filepath.Join(w.repo, rel)}
	if content != nil {
		input["content"] = *content
	}
	payload, err := json.Marshal(map[string]any{
		"session_id": w.session,
		"cwd":        w.repo,
		"tool_name":  tool,
		"tool_input": input,
	})
	if err != nil {
		w.t.Fatal(err)
	}
	cmd := exec.Command(w.bin, mode)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"CLAUDE_PROJECT_DIR="+w.repo,
		"JEVONS_TREEGUARD_DIR="+w.store(),
		"JEVONS_TREEGUARD_PATHS="+fixtureName,
	)
	if !w.guard {
		cmd.Env = append(cmd.Env, "JEVONS_TREEGUARD=off")
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, errBuf.String()
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), errBuf.String()
	default:
		w.t.Fatalf("running guard: %v", err)
		return 0, ""
	}
}

func replaceOnce(t *testing.T, content []byte, marker, replacement string) []byte {
	t.Helper()
	s := string(content)
	if !strings.Contains(s, marker) {
		t.Fatalf("marker %q absent from content", marker)
	}
	return []byte(strings.Replace(s, marker, replacement, 1))
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-c", "user.email=treeguard@test", "-c", "user.name=treeguard",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitShowHead(t *testing.T, dir, rel string) string {
	t.Helper()
	cmd := exec.Command("git", "show", "HEAD:"+rel)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show HEAD:%s: %v", rel, err)
	}
	return string(out)
}
