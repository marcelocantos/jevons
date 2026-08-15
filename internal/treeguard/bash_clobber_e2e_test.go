package treeguard_test

// 🎯T391 acceptance oracle: the guard defends a BOUNDARY, not a tool.
//
// 🎯T376's oracle drives the Write tool. This one drives a shell command, which
// is the path that walked past the guard: `> file`, `sed -i`, `tee`, a heredoc.
// The scenario is deliberately the same loss as T376 — two workers, disjoint
// regions of one hot file — so that the only variable is HOW the second worker
// writes.
//
// Four tests, and the last two are the point. A guard is wrong in two
// directions and a green test that only checks one of them is worth nothing:
//
//	TestBashClobberIsRefusedAndNamesTheLoss   the property (guard on)
//	TestBashClobberLandsWhenBashGuardOff      RED control: too NARROW. Runs the
//	    identical interleaving with JEVONS_TREEGUARD_BASH=off — the pre-fix
//	    behaviour — and asserts worker A's region is GONE from HEAD. If this
//	    ever passes-by-not-clobbering, the scenario has stopped reproducing the
//	    defect and the test above proves nothing.
//	TestBashGuardAllowsLegitimateWrites       the other direction: read-only
//	    commands, unguarded paths, and an up-to-date worker's append all pass.
//	TestOverBroadGuardFailsTheLegitimateOracle RED control: too WIDE. Runs the
//	    identical assertions against a mutant guard that refuses everything, and
//	    requires them to FAIL. A guard that denies every write satisfies the
//	    first test perfectly and is useless; without this control nothing in the
//	    package notices.
//
// Both controls drive a mutant rather than describing one, because a control
// that needs a human to set an env var is a comment with a build tag.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// unguardedName is a second file in the fixture repo that the guard does not
// cover, so "allowed" can be distinguished from "not looked at".
const unguardedName = "notes.txt"

// MutateEnv turns the two property tests against a broken guard so the RED
// controls can be RUN rather than argued. The always-on control tests above
// already assert each mutant's behaviour; this knob additionally lets the
// SAME oracle be pointed at the mutation, which is the evidence that it fails
// for the right reason:
//
//	T391_ORACLE_MUTATE=bash-off  TestBashClobberIsRefusedAndNamesTheLoss  → RED
//	    (the pre-fix tree: a guard that never inspects a shell command)
//	T391_ORACLE_MUTATE=deny-all  TestBashGuardAllowsLegitimateWrites      → RED
//	    (an over-broad guard that refuses legitimate writes)
//
// Unset — the committed default — both are GREEN against the real guard.
const MutateEnv = "T391_ORACLE_MUTATE"

const (
	mutateBashOff = "bash-off"
	mutateDenyAll = "deny-all"
)

// preFixBashOff reports whether this run is mutated to the pre-fix behaviour.
func preFixBashOff() bool { return os.Getenv(MutateEnv) == mutateBashOff }

func TestBashClobberIsRefusedAndNamesTheLoss(t *testing.T) {
	repo, bin := newScenario(t)
	a := &worker{t: t, bin: bin, repo: repo, session: "worker-a", guard: true}
	b := &worker{t: t, bin: bin, repo: repo, session: "worker-b", guard: true}

	// Both read the same base: this is the window in which the loss happens.
	baseA := a.read(fixtureName)
	baseB := b.read(fixtureName)

	a.mustWrite(fixtureName, replaceOnce(t, baseA, regionAMarker, regionAEdit))
	a.commit(fixtureName, "worker A: fleet_cycle script tag")

	// B still holds the pre-A content and rewrites the whole file through a
	// shell redirection. Nothing about this reaches the Write tool.
	stale := replaceOnce(t, baseB, regionBMarker, regionBEdit)
	allowed, stderr := b.bashAttempt(clobberCommand(stale), preFixBashOff())
	if allowed {
		t.Fatal("guard allowed a stale full-file write through Bash; 🎯T391's gap is open")
	}
	if !strings.Contains(stderr, "fleet_cycle.js") {
		t.Errorf("refusal did not name the at-risk line, so the loss is still hard to diagnose:\n%s", stderr)
	}
	if !strings.Contains(stderr, fixtureName) {
		t.Errorf("refusal did not name the file it protected:\n%s", stderr)
	}

	// The refusal is recoverable the same way the Write-tool one is: re-read,
	// re-apply, write. A guard the worker cannot get past is a broken guard.
	freshB := b.read(fixtureName)
	if allowed, stderr := b.bashAttempt(clobberCommand(replaceOnce(t, freshB, regionBMarker, regionBEdit)), preFixBashOff()); !allowed {
		t.Fatalf("guard refused a re-read, up-to-date shell write; the refusal is not recoverable:\n%s", stderr)
	}
	b.commit(fixtureName, "worker B: sidebar composer title")

	head := gitShowHead(t, repo, fixtureName)
	if !strings.Contains(head, "fleet_cycle.js") {
		t.Error("worker A's edit is absent at HEAD")
	}
	if !strings.Contains(head, "sidebar-composer-title") {
		t.Error("worker B's edit is absent at HEAD")
	}
}

// TestBashClobberLandsWhenBashGuardOff is the RED control for the test above:
// the same interleaving against the pre-fix behaviour, asserting the loss.
func TestBashClobberLandsWhenBashGuardOff(t *testing.T) {
	repo, bin := newScenario(t)
	a := &worker{t: t, bin: bin, repo: repo, session: "worker-a", guard: true}
	b := &worker{t: t, bin: bin, repo: repo, session: "worker-b", guard: true}

	baseA := a.read(fixtureName)
	baseB := b.read(fixtureName)

	a.mustWrite(fixtureName, replaceOnce(t, baseA, regionAMarker, regionAEdit))
	a.commit(fixtureName, "worker A: fleet_cycle script tag")

	stale := replaceOnce(t, baseB, regionBMarker, regionBEdit)
	if allowed, stderr := b.bashAttempt(clobberCommand(stale), true); !allowed {
		t.Fatalf("guard refused with %s=off; the control proves nothing:\n%s", "JEVONS_TREEGUARD_BASH", stderr)
	}
	b.commit(fixtureName, "worker B: sidebar composer title")

	head := gitShowHead(t, repo, fixtureName)
	if strings.Contains(head, "fleet_cycle.js") {
		t.Error("the unguarded shell write did NOT clobber worker A — the scenario no longer reproduces the defect, so the guarded test proves nothing")
	}
	if !strings.Contains(head, "sidebar-composer-title") {
		t.Error("worker B's own edit is missing; the control scenario is broken")
	}
}

// TestBashGuardAllowsLegitimateWrites pins the over-broadness direction. A
// guard that refuses these teaches workers to set JEVONS_TREEGUARD_BASH=off,
// which is strictly worse than the gap it closes.
func TestBashGuardAllowsLegitimateWrites(t *testing.T) {
	bin := ""
	if os.Getenv(MutateEnv) == mutateDenyAll {
		bin = mutantAlwaysDeny(t)
	}
	for _, f := range runLegitimateWrites(t, bin) {
		t.Error(f)
	}
}

// TestOverBroadGuardFailsTheLegitimateOracle is the RED control for the test
// above. It runs the identical assertions against a guard that refuses every
// write and requires them to fail: that is what makes the green above evidence
// of discrimination rather than evidence of a guard that is simply on.
func TestOverBroadGuardFailsTheLegitimateOracle(t *testing.T) {
	failures := runLegitimateWrites(t, mutantAlwaysDeny(t))
	if len(failures) == 0 {
		t.Fatal("a guard that refuses EVERY write passed the legitimate-write oracle, so that oracle cannot detect over-broadness and its green means nothing")
	}
	t.Logf("over-broad mutant correctly caught by %d assertion(s); first: %s", len(failures), failures[0])
}

// runLegitimateWrites drives the allow-side cases against the guard binary at
// bin (empty = the real one) and RETURNS failures rather than reporting them,
// so the same body serves as both the property and its mutation control.
func runLegitimateWrites(t *testing.T, bin string) []string {
	t.Helper()
	repo, realBin := newScenario(t)
	if bin == "" {
		bin = realBin
	}
	w := &worker{t: t, bin: bin, repo: repo, session: "worker-legit", guard: true}

	// The worker holds current content for the guarded file, so every write it
	// makes below is a compare-and-swap that should succeed.
	current := w.read(fixtureName)

	cases := []struct {
		what    string
		command string
	}{
		{"a read-only grep naming the guarded file", "grep -n REGION " + fixtureName},
		{"a sed WITHOUT -i (prints, does not rewrite)", "sed -n '1,3p' " + fixtureName},
		{"a diff reading the guarded file", "git diff -- " + fixtureName},
		{"a write to a file the guard does not cover", "printf 'scratch\\n' > " + unguardedName},
		{"an append by a worker holding current content", "printf '<!-- appended -->\\n' >> " + fixtureName},
		{"a full rewrite by a worker holding current content", clobberCommand(append(current, []byte("<!-- rewritten -->\n")...))},
	}

	var failures []string
	for _, tc := range cases {
		allowed, stderr := w.bashAttempt(tc.command, false)
		if !allowed {
			failures = append(failures, fmt.Sprintf("guard refused %s — over-broad refusals drive workers to disable it:\n  command: %s\n  said: %s", tc.what, tc.command, strings.TrimSpace(stderr)))
		}
	}
	return failures
}

// mutantAlwaysDeny builds a guard that refuses every pre-write. It allows post
// so the fixture's own bookkeeping still runs and the only variable under test
// is the refusal.
func mutantAlwaysDeny(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "always-deny")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = pre ]; then echo 'mutant guard: refusing everything' >&2; exit 2; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// clobberCommand is the shell form of a full-file write: the construct a worker
// reaches for when it is not using the Write tool.
func clobberCommand(content []byte) string {
	return fmt.Sprintf("printf '%%s' '%s' > %s", strings.ReplaceAll(string(content), "'", "'\\''"), fixtureName)
}

// bashAttempt runs the guard over a shell command and, if allowed, actually
// executes it — the write must really happen, or the HEAD assertions in the
// control would pass for the wrong reason.
func (w *worker) bashAttempt(command string, bashOff bool) (allowed bool, stderr string) {
	w.t.Helper()
	exit, out := w.bashHook("pre", command, bashOff)
	switch exit {
	case 0:
	case 2:
		return false, out
	default:
		w.t.Fatalf("pre-bash hook failed: exit %d: %s", exit, out)
	}

	run := exec.Command("sh", "-c", command)
	run.Dir = w.repo
	if combined, err := run.CombinedOutput(); err != nil {
		w.t.Fatalf("running the allowed command %q: %v: %s", command, err, combined)
	}

	if exit, out := w.bashHook("post", command, bashOff); exit != 0 {
		w.t.Fatalf("post-bash hook exit %d: %s", exit, out)
	}
	return true, ""
}

func (w *worker) bashHook(mode, command string, bashOff bool) (exit int, stderr string) {
	w.t.Helper()
	payload, err := json.Marshal(map[string]any{
		"session_id": w.session,
		"cwd":        w.repo,
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": command},
	})
	if err != nil {
		w.t.Fatal(err)
	}
	cmd := exec.Command(w.bin, mode)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Dir = w.repo
	cmd.Env = append(os.Environ(),
		"CLAUDE_PROJECT_DIR="+w.repo,
		"JEVONS_TREEGUARD_DIR="+w.store(),
		"JEVONS_TREEGUARD_PATHS="+fixtureName,
	)
	if bashOff {
		cmd.Env = append(cmd.Env, "JEVONS_TREEGUARD_BASH=off")
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
