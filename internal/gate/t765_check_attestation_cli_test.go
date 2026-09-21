// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T765.1: `gate check-attestation` judges a proposed achieve before it is
// written. These run the shipped binary against a throwaway git repo holding
// the ledger — never the live ledger — with the process cwd in the jevons
// tree, so a check that resolved commits against cwd instead of the ledger's
// own repo would fail every case.

type t765Repo struct {
	dir, ledger      string
	parent, fix, kid string // full shas: parent ← fix ← kid
}

func newT765Repo(t *testing.T) t765Repo {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "parent")
	r := t765Repo{dir: dir, ledger: filepath.Join(dir, "bullseye.yaml")}
	r.parent = git("rev-parse", "HEAD")
	git("commit", "-q", "--allow-empty", "-m", "fix")
	r.fix = git("rev-parse", "HEAD")
	git("commit", "-q", "--allow-empty", "-m", "descendant")
	r.kid = git("rev-parse", "HEAD")
	if err := os.WriteFile(r.ledger, []byte("targets:\n  T9:\n    status: converging\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return r
}

func saveT765Record(t *testing.T, store, id, commit string, verdict gate.Verdict, exit int, clean bool) {
	t.Helper()
	s, err := gate.OpenStore(store)
	if err != nil {
		t.Fatal(err)
	}
	dirty := 0
	if !clean {
		dirty = 4
	}
	rec := &gate.Record{
		ID: id, Name: "t765", StatusKnown: true, ExitStatus: exit, Verdict: verdict,
		Tree: &gate.TreeProvenance{Commit: commit, Clean: clean, DirtyFiles: dirty},
	}
	if err := s.Save(rec); err != nil {
		t.Fatal(err)
	}
}

func checkAttestation(t *testing.T, store string, r t765Repo, id, attestation string) (int, string) {
	t.Helper()
	cmd := exec.Command(gateBinary(t), "check-attestation", "-ledger", r.ledger, "-id", id)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	cmd.Stdin = strings.NewReader(attestation)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, isExit := errAsExit(err); !isExit {
			t.Fatalf("running check-attestation: %v (%s)", err, out)
		}
	}
	return cmd.ProcessState.ExitCode(), string(out)
}

func TestCheckAttestationAcceptance1(t *testing.T) {
	r := newT765Repo(t)
	store := t.TempDir()
	fix := r.fix[:10]
	saveT765Record(t, store, "aaaa1111", r.kid, gate.VerdictGreen, 0, true)
	saveT765Record(t, store, "bbbb2222", r.kid, gate.VerdictDirty, 0, false)
	saveT765Record(t, store, "cccc3333", r.kid, gate.VerdictRed, 1, true)
	saveT765Record(t, store, "dddd4444", r.parent, gate.VerdictGreen, 0, true)
	saveT765Record(t, store, "eeee5555", r.kid, gate.VerdictGreen, 0, false) // GREEN verdict, unclean tree

	cases := []struct {
		name, attestation string
		wantExit          int
		wantAll           []string
	}{
		{"missing gate id", "Fixed in " + fix + "; clean gate green.", 4,
			[]string{"refused", "achieve_gate_uncited", "field=gate_id"}},
		{"unknown gate id", "Fixed in " + fix + ". GATE t9 exit=0 GREEN id=ffff6666", 4,
			[]string{"refused", "field=gate_id"}},
		{"misquoted verdict", "Fixed in " + fix + ". GATE t9 exit=0 GREEN id=bbbb2222", 4,
			[]string{"refused", "attestation_contradicted", "field=verdict"}},
		{"non-GREEN record", "Fixed in " + fix + ". GATE t9 exit=1 RED id=cccc3333", 4,
			[]string{"refused", "field=verdict"}},
		{"tree.clean false", "Fixed in " + fix + ". GATE t9 exit=0 GREEN id=eeee5555", 4,
			[]string{"refused", "field=tree.clean"}},
		{"gate on a commit lacking the fix", "Fixed in " + fix + ". GATE t9 exit=0 GREEN id=dddd4444", 4,
			[]string{"refused", "achieve_gate_precedes_fix", "field=tree.commit"}},
		{"clean GREEN on a descendant", "Fixed in " + fix + ". GATE t9 exit=0 GREEN id=aaaa1111", 0,
			[]string{"T9: verified"}},
		{"no gate claimed", "Reviewed by hand; no build involved.", 0,
			[]string{"ungated"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out := checkAttestation(t, store, r, "T9", c.attestation)
			if code != c.wantExit {
				t.Fatalf("exit %d, want %d\n%s", code, c.wantExit, out)
			}
			for _, w := range c.wantAll {
				if !strings.Contains(out, w) {
					t.Errorf("output missing %q:\n%s", w, out)
				}
			}
		})
	}
}

// Accepted risk about the gate passes but never reads as verified, and never
// launders a citation of a run that did not happen as described.
func TestCheckAttestationMarkedIsVisiblyDistinct(t *testing.T) {
	r := newT765Repo(t)
	store := t.TempDir()
	fix := r.fix[:10]
	saveT765Record(t, store, "bbbb2222", r.kid, gate.VerdictDirty, 0, false)

	code, out := checkAttestation(t, store, r, "T9",
		"Fixed in "+fix+". GATE t9 DIRTY id=bbbb2222. Accepted-risk: clean gate impossible here, SDL3 libs missing.")
	if code != 0 {
		t.Fatalf("marked achieve exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, gate.AchieveMarker) || strings.Contains(out, "T9: verified") {
		t.Fatalf("marked achieve is not visibly distinct from verified:\n%s", out)
	}

	code, out = checkAttestation(t, store, r, "T9",
		"Fixed in "+fix+". GATE t9 exit=0 GREEN id=bbbb2222. Accepted-risk: dirty gate.")
	if code != 4 || strings.Contains(out, gate.AchieveMarker) {
		t.Fatalf("accepted-risk laundered a misquote: exit %d\n%s", code, out)
	}
}

// A check that cannot be made is not a pass: it exits 70 (or 2 for a bad
// command line), never 0.
func TestCheckAttestationCannotJudgeIsNotAPass(t *testing.T) {
	r := newT765Repo(t)
	store := t.TempDir()
	att := "GATE t9 exit=0 GREEN id=aaaa1111"

	if code, out := checkAttestation(t, store, r, "T404", att); code != 70 || !strings.Contains(out, "not in") {
		t.Fatalf("unknown target: exit %d, want 70\n%s", code, out)
	}
	notGit := t.TempDir()
	led := filepath.Join(notGit, "bullseye.yaml")
	if err := os.WriteFile(led, []byte("targets:\n  T9: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(gateBinary(t), "check-attestation", "-ledger", led, "-id", "T9")
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store, "GIT_CEILING_DIRECTORIES="+filepath.Dir(notGit))
	cmd.Stdin = strings.NewReader(att)
	if _, err := cmd.CombinedOutput(); err == nil || cmd.ProcessState.ExitCode() != 70 {
		t.Fatalf("ledger outside git: exit %d, want 70", cmd.ProcessState.ExitCode())
	}
	if code, _ := runGate(t, store, "check-attestation", "-id", "T9"); code != 2 {
		t.Fatalf("missing -ledger: exit %d, want 2", code)
	}
}
