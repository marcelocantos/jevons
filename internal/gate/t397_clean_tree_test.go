// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T397 acceptance 4: "a fixture commit that is green in a dirty tree and red
// on clean checkout is detected and reported. Red against the pre-fix tree,
// plus an over-broadness mutation so a legitimately self-consistent commit is
// not flagged."
//
// The fixture is the incident in miniature. defs.txt is a neighbour's file;
// the commit under test needs a symbol from it; the neighbour has written the
// symbol but not committed it. Every worker in the clone reads green, and a
// fresh clone, CI, a bisect or a release build reads red.
//
// Deliberately not written in Go. The property has nothing to do with
// compilation — it is about which files are in the tree the gate was handed —
// and a grep makes each case a hundred milliseconds instead of a build, which
// is what keeps this in the standing suite rather than in a nightly.

// requiredSymbol is what the commit under test needs from the neighbour's file.
const requiredSymbol = "SYMBOL_THE_COMMIT_NEEDS"

// fixtureRepo builds a git repo whose HEAD is missing that symbol, and returns
// its path. The uncommitted half is left to the caller: the two cases this
// file distinguishes differ only in whether the symbol is committed or lying
// around.
func fixtureRepo(t *testing.T, commitTheSymbol bool) (root, head string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root = filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create fixture root: %v", err)
	}
	fixtureGit(t, root, "init", "-q")

	body := "package neighbour\n"
	if commitTheSymbol {
		body += requiredSymbol + "\n"
	}
	writeFixture(t, root, "defs.txt", body)
	// The commit under test: a file that asserts it needs the symbol. What it
	// says does not matter; that it is committed and defs.txt's symbol is not
	// is the whole fixture.
	writeFixture(t, root, "caller.txt", "needs "+requiredSymbol+"\n")
	fixtureGit(t, root, "add", "defs.txt", "caller.txt")
	fixtureGit(t, root,
		"-c", "user.email=fixture@example.com", "-c", "user.name=Fixture",
		"-c", "commit.gpgsign=false", "commit", "-q", "-m", "the commit under test")

	out := fixtureGit(t, root, "rev-parse", "HEAD")
	return root, strings.TrimSpace(out)
}

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeFixture(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// symbolCheck is the gate command: it passes when the symbol is present in the
// tree the command was handed.
func symbolCheck() []string {
	return []string{"grep", "-q", requiredSymbol, "defs.txt"}
}

// TestT397DirtyTreeIsGreenAndCleanCheckoutIsRed is the oracle. The shared-tree
// reading is the control: it shows the defect is real and that nothing in the
// pre-fix world detects it — the worker who runs the gate where they are
// standing gets a green, honestly, for a commit that is red on its own.
func TestT397DirtyTreeIsGreenAndCleanCheckoutIsRed(t *testing.T) {
	root, head := fixtureRepo(t, false)
	// The neighbour's uncommitted work: the symbol exists on disk and in
	// nobody's commit.
	writeFixture(t, root, "defs.txt", "package neighbour\n"+requiredSymbol+"\n")

	store := storeAt(t)

	// CONTROL — the pre-fix reading, taken in the tree the worker is standing
	// in. Green, and wrong about the commit.
	dirty, err := Run(&RunArgs{Command: symbolCheck(), Dir: root, Store: store, Stdout: nil, Stderr: nil})
	if err != nil {
		t.Fatalf("shared-tree run: %v", err)
	}
	if !dirty.Verdict.IsGreen() {
		t.Fatalf("the fixture does not reproduce the incident: the shared tree should read GREEN, got %s", dirty.Summary())
	}
	if dirty.Tree == nil || dirty.Tree.Clean || dirty.Tree.DirtyFiles == 0 {
		t.Fatalf("a run in a dirty tree must record it as dirty, got %+v", dirty.Tree)
	}
	if dirty.Tree.Commit != head {
		t.Errorf("dirty run recorded commit %q, want %q", dirty.Tree.Commit, head)
	}
	if !strings.Contains(dirty.Attestation(), "tree=dirty+") {
		t.Errorf("attestation does not say which tree it measured: %s", dirty.Attestation())
	}

	// THE MECHANISM — the same gate, the same commit, its own checkout.
	res, err := RunClean(&CleanArgs{
		Command: symbolCheck(), Repo: root, WorkDir: t.TempDir(),
		Store: store, Stdout: nil, Stderr: nil,
	})
	if err != nil {
		t.Fatalf("clean run: %v", err)
	}
	clean := res.Record
	if clean.Verdict.IsGreen() {
		t.Fatalf("a commit that is red on its own attested GREEN from a clean checkout: %s", clean.Summary())
	}
	if clean.Verdict != VerdictRed || clean.Status() != "1" {
		t.Errorf("clean run = %s, want a RED with the command's own status", clean.Summary())
	}
	if clean.Tree == nil || !clean.Tree.Clean {
		t.Fatalf("a clean run must record a clean tree, got %+v", clean.Tree)
	}
	if clean.Tree.Commit != head {
		t.Errorf("clean run measured %q, want the repo's HEAD %q", clean.Tree.Commit, head)
	}
	// The record says what was left out, so the reading is reconstructable
	// from the evidence rather than from memory of the working tree.
	if clean.Tree.Shared == nil || clean.Tree.Shared.DirtyFiles == 0 {
		t.Errorf("clean run does not record what the shared tree was carrying: %+v", clean.Tree)
	}
	if !strings.Contains(clean.Attestation(), "tree=clean@") {
		t.Errorf("attestation does not carry the clean-tree token: %s", clean.Attestation())
	}
}

// TestT397SelfConsistentCommitIsGreenBothWays is the over-broadness mutation.
// The same machinery, on a commit that carries everything it needs, must reach
// the same verdict as the shared tree — a checker that reds a sound commit
// would be switched off inside a day.
func TestT397SelfConsistentCommitIsGreenBothWays(t *testing.T) {
	root, _ := fixtureRepo(t, true)
	store := storeAt(t)

	dirty, err := Run(&RunArgs{Command: symbolCheck(), Dir: root, Store: store})
	if err != nil {
		t.Fatalf("shared-tree run: %v", err)
	}
	if !dirty.Verdict.IsGreen() {
		t.Fatalf("self-consistent commit is not green in the shared tree: %s", dirty.Summary())
	}
	res, err := RunClean(&CleanArgs{
		Command: symbolCheck(), Repo: root, WorkDir: t.TempDir(), Store: store,
	})
	if err != nil {
		t.Fatalf("clean run: %v", err)
	}
	if !res.Record.Verdict.IsGreen() {
		t.Fatalf("a commit that carries everything it needs came out %s — the clean run "+
			"is failing sound work, which is the failure mode that gets a guard disabled",
			res.Record.Summary())
	}
	if res.Record.Tree == nil || !res.Record.Tree.Clean {
		t.Fatalf("clean run recorded %+v, want a clean tree", res.Record.Tree)
	}
}

// TestT397ReportCitingADirtyGateIsFlagged closes the loop at the integration
// point (acceptance 2): the failure is visible to the reader of the report,
// not only to the worker who thought to check.
func TestT397ReportCitingADirtyGateIsFlagged(t *testing.T) {
	root, head := fixtureRepo(t, false)
	writeFixture(t, root, "defs.txt", "package neighbour\n"+requiredSymbol+"\n")
	store := storeAt(t)

	rec, err := Run(&RunArgs{Command: symbolCheck(), Dir: root, Store: store})
	if err != nil {
		t.Fatalf("shared-tree run: %v", err)
	}
	report := "🎯T999 done. Commit `" + head[:7] + "` on local master.\n\n" +
		"    " + rec.Attestation() + "\n"

	flags := FlagFalseGreen(report, store.Lookup)
	var got *Flag
	for i := range flags {
		if flags[i].Kind == FlagDirtyTreeGate {
			got = &flags[i]
		}
	}
	if got == nil {
		t.Fatalf("a green cited for a commit, measured in a dirty tree, was not flagged; flags=%v", flags)
	}
	for _, want := range []string{"🎯T397", "bin/gate -clean", head[:7]} {
		if want == head[:7] {
			// The banner quotes the commit the gate actually measured, which is
			// the same commit — checked via the short form the record carries.
			want = rec.Tree.ShortCommit()[:7]
		}
		if !strings.Contains(got.Detail, want) {
			t.Errorf("flag detail does not mention %q: %s", want, got.Detail)
		}
	}
	if !strings.Contains(Banner(flags), "dirty_tree_gate") {
		t.Errorf("the banner does not carry the flag: %s", Banner(flags))
	}
}

// TestT397NarrownessOfTheReportCheck is the other half of over-broadness, at
// report time. Each case here is a report the checker must stay silent about,
// and each one would fire under an obvious simplification of the rule.
func TestT397NarrownessOfTheReportCheck(t *testing.T) {
	root, head := fixtureRepo(t, false)
	writeFixture(t, root, "defs.txt", "package neighbour\n"+requiredSymbol+"\n")
	store := storeAt(t)

	dirtyRec, err := Run(&RunArgs{Command: symbolCheck(), Dir: root, Store: store})
	if err != nil {
		t.Fatalf("shared-tree run: %v", err)
	}
	cleanRes, err := RunClean(&CleanArgs{
		Command: []string{"grep", "-q", "neighbour", "defs.txt"},
		Repo:    root, WorkDir: t.TempDir(), Store: store,
	})
	if err != nil {
		t.Fatalf("clean run: %v", err)
	}

	// A record from before the field existed: provenance unknown. Vouching for
	// it either way is the mistake; silence is the answer.
	unknown := *dirtyRec
	unknown.ID = "unknown1"
	unknown.Tree = nil
	if err := store.Save(&unknown); err != nil {
		t.Fatalf("save: %v", err)
	}

	for _, tc := range []struct {
		name   string
		report string
	}{
		{"clean gate cited for a commit", "Done, commit " + head[:7] + ".\n" + cleanRes.Record.Attestation()},
		{"unknown provenance", "Done, commit " + head[:7] + ".\n" + unknown.Attestation()},
		{"no commit claimed", "Read of the current tree:\n" + dirtyRec.Attestation()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, f := range FlagFalseGreen(tc.report, store.Lookup) {
				if f.Kind == FlagDirtyTreeGate {
					t.Errorf("flagged an honest report: %s", f)
				}
			}
		})
	}
}

// TestT397CleanRunRefusesRatherThanFallingBack: asked for a clean tree where
// none can be made, the runner fails. The tempting alternative — run in the
// shared tree and note it — would file a record under the clean-run flag for a
// reading that is exactly what the flag exists to avoid.
func TestT397CleanRunRefusesRatherThanFallingBack(t *testing.T) {
	notARepo := t.TempDir()
	store := storeAt(t)
	res, err := RunClean(&CleanArgs{
		Command: []string{"true"}, Repo: notARepo, WorkDir: t.TempDir(), Store: store,
	})
	if err == nil {
		t.Fatalf("RunClean vouched for a tree it could not make: %+v", res)
	}
	recs, rerr := store.Recent(10)
	if rerr != nil {
		t.Fatalf("recent: %v", rerr)
	}
	if len(recs) != 0 {
		t.Errorf("a refused clean run left %d citable record(s) behind", len(recs))
	}
}

// TestT397CleanRunLeavesNoWorktreeBehind keeps the mechanism from becoming a
// leak generator in the clone every worker shares (🎯T440's subject).
func TestT397CleanRunLeavesNoWorktreeBehind(t *testing.T) {
	root, _ := fixtureRepo(t, true)
	before := fixtureGit(t, root, "worktree", "list")
	if _, err := RunClean(&CleanArgs{
		Command: symbolCheck(), Repo: root, WorkDir: t.TempDir(), Store: storeAt(t),
	}); err != nil {
		t.Fatalf("clean run: %v", err)
	}
	if after := fixtureGit(t, root, "worktree", "list"); after != before {
		t.Errorf("clean run left a worktree behind:\nbefore:\n%safter:\n%s", before, after)
	}
}

// storeAt is a throwaway record store for one test.
func storeAt(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}
