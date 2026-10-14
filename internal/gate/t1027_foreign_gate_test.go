// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T1027: a gate id cited as evidence is checked against the citing repo.
// The store under ~/.jevons/gates is one namespace for every repo on the
// machine; on 2026-10-07 an arrai RED gate (8c015098) was attributed to a
// jevons worker, and nothing in the checker could have said "that record is
// not even this repo's" had the worker really cited it.

// t1027Repo makes a one-commit repository and returns its root and HEAD.
func t1027Repo(t *testing.T, name string) (root, head string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root = filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
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
	if err := os.WriteFile(filepath.Join(root, name+".txt"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", name+".txt")
	git("-c", "commit.gpgsign=false", "commit", "-q", "-m", name)
	return root, git("rev-parse", "HEAD")
}

// runGateIn records a real passing gate whose tree provenance is dir.
func runGateIn(t *testing.T, store *gate.Store, dir, name string) *gate.Record {
	t.Helper()
	rec, err := gate.Run(&gate.RunArgs{
		Command: []string{"true"},
		Name:    name,
		Dir:     dir,
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Tree == nil || rec.Tree.Commit == "" {
		t.Fatalf("gate in %s recorded no tree provenance: %+v", dir, rec.Tree)
	}
	return rec
}

func kinds(flags []gate.Flag) []string {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		out = append(out, string(f.Kind))
	}
	return out
}

// The incident shape, with the roles made explicit: a gate that ran in repo
// "arrai" is cited from repo "jevons". The record is GREEN on purpose — the
// RED is what the existing checker would have caught; the foreign origin is
// what nothing caught.
func TestT1027GateFromAnotherRepoIsFlaggedForeign(t *testing.T) {
	store, err := gate.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	jevons, _ := t1027Repo(t, "jevons")
	arrai, _ := t1027Repo(t, "arrai")
	rec := runGateIn(t, store, arrai, "arrai-t41-slowpath-corpus")
	if rec.Verdict != gate.VerdictGreen {
		t.Fatalf("control gate is %s, want GREEN", rec.Verdict)
	}
	report := "🎯T1024 done; the reap subsystem is green.\n\n    " + rec.Attestation() + "\n"

	citing, ok := gate.CommitKnownIn(jevons)
	if !ok {
		t.Fatal("CommitKnownIn refused a real repo")
	}
	flags := gate.FlagForeignGates(report, store.Lookup, citing)
	if len(flags) != 1 || flags[0].Kind != gate.FlagForeignRepoGate {
		t.Fatalf("foreign citation not flagged: %v", kinds(flags))
	}
	f := flags[0]
	for _, want := range []string{rec.ID, rec.Name, rec.Tree.ShortCommit(), arrai, "🎯T1027"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("detail lacks %q:\n%s", want, f.Detail)
		}
	}
	// Evidence is the citation as the attestation parser sees it: the GATE
	// line up to its id, which is a prefix of the full line the worker pasted.
	if f.Evidence == "" || !strings.HasPrefix(rec.Attestation(), f.Evidence) || !strings.Contains(f.Evidence, "id="+rec.ID) {
		t.Errorf("evidence = %q, want the cited GATE line", f.Evidence)
	}

	// Control: the same citation from the repo that ran it is not foreign.
	own, _ := gate.CommitKnownIn(arrai)
	if flags := gate.FlagForeignGates(report, store.Lookup, own); len(flags) != 0 {
		t.Fatalf("a worker's own gate was flagged foreign: %v", flags)
	}
	// Control: a green citation from the same repo keeps the whole checker silent.
	if flags := gate.FlagFalseGreen(report, store.Lookup); len(flags) != 0 {
		t.Fatalf("control report tripped FlagFalseGreen: %v", flags)
	}
}

// The envelope gate-id slot and declared gate-role ids are citations too —
// a worker that puts the foreign id in `jevons: gate-id` and never pastes a
// GATE line is making the same claim.
func TestT1027EnvelopeSlotsAreScoped(t *testing.T) {
	store, err := gate.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	jevons, _ := t1027Repo(t, "jevons")
	arrai, _ := t1027Repo(t, "arrai")
	pass := runGateIn(t, store, arrai, "arrai-pass")
	ctrl := runGateIn(t, store, arrai, "arrai-before")
	report := "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T1\n" +
		"jevons: gate-id " + pass.ID + "\n" +
		"jevons: verdict GREEN\n" +
		"jevons: gate-role id=" + ctrl.ID + " role=control\n" +
		"jevons: silent-ledger none\n" +
		"```\nDone.\n"
	citing, _ := gate.CommitKnownIn(jevons)
	flags := gate.FlagForeignGates(report, store.Lookup, citing)
	got := map[string]string{}
	for _, f := range flags {
		if f.Kind != gate.FlagForeignRepoGate {
			t.Errorf("unexpected kind %s", f.Kind)
		}
		got[f.Evidence] = f.Detail
	}
	if _, ok := got["jevons: gate-id "+pass.ID]; !ok {
		t.Errorf("gate-id slot not flagged: %v", flags)
	}
	if _, ok := got["jevons: gate-role "+ctrl.ID]; !ok {
		t.Errorf("gate-role id not flagged: %v", flags)
	}
	if len(flags) != 2 {
		t.Errorf("want exactly two flags, got %d: %v", len(flags), kinds(flags))
	}
}

// What the check must stay silent on: an id with no record (that is
// attestation_unknown's job), a record with no tree provenance (unknown is
// never foreign), and a caller with no repo to scope to.
func TestT1027UnknownIsNeverForeign(t *testing.T) {
	store, err := gate.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	jevons, _ := t1027Repo(t, "jevons")
	noTree := &gate.Record{
		ID: "deadbeef", Name: "sh-c", Command: []string{"sh", "-c", "true"},
		StatusKnown: true, Verdict: gate.VerdictGreen,
	}
	if err := store.Save(noTree); err != nil {
		t.Fatal(err)
	}
	report := "Done.\n\n    " + noTree.Attestation() + "\n    GATE ghost exit=0 GREEN id=0badf00d out=00 dur=1s\n"
	citing, _ := gate.CommitKnownIn(jevons)
	if flags := gate.FlagForeignGates(report, store.Lookup, citing); len(flags) != 0 {
		t.Fatalf("unknown provenance or unknown id flagged foreign: %v", flags)
	}
	if flags := gate.FlagForeignGates(report, store.Lookup, nil); len(flags) != 0 {
		t.Fatalf("nil known func produced flags: %v", flags)
	}
	if _, ok := gate.CommitKnownIn(t.TempDir()); ok {
		t.Fatal("CommitKnownIn accepted a directory that is not a work tree")
	}
	if _, ok := gate.CommitKnownIn(""); ok {
		t.Fatal("CommitKnownIn accepted an empty path")
	}
}

// jevons_gate_show's scope answer, as a pure classification.
func TestT1027ScopeOf(t *testing.T) {
	store, err := gate.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	jevons, _ := t1027Repo(t, "jevons")
	arrai, _ := t1027Repo(t, "arrai")
	rec := runGateIn(t, store, arrai, "arrai-gate")

	if sc := gate.ScopeOf(rec, arrai); sc.Verdict != gate.ScopeOwn || !strings.HasPrefix(sc.Describe(), "scope: own") {
		t.Errorf("own repo: %+v / %s", sc, sc.Describe())
	}
	sc := gate.ScopeOf(rec, jevons)
	if sc.Verdict != gate.ScopeForeign {
		t.Fatalf("foreign repo: %+v", sc)
	}
	for _, want := range []string{"scope: FOREIGN", arrai, jevons, "🎯T1027"} {
		if !strings.Contains(sc.Describe(), want) {
			t.Errorf("foreign description lacks %q: %s", want, sc.Describe())
		}
	}
	if sc := gate.ScopeOf(rec, t.TempDir()); sc.Verdict != gate.ScopeUnknown {
		t.Errorf("non-repo caller: %+v", sc)
	}
	if sc := gate.ScopeOf(&gate.Record{ID: "x"}, jevons); sc.Verdict != gate.ScopeUnknown || !strings.Contains(sc.Describe(), "no tree provenance") {
		t.Errorf("record without provenance: %+v / %s", sc, sc.Describe())
	}
}

// The banner names the report it judged. The 2026-10-07 misattribution was
// an anonymous banner read as the previous report's.
func TestT1027BannerNamesTheReportAuthor(t *testing.T) {
	flags := []gate.Flag{{Kind: gate.FlagForeignRepoGate, Detail: "d", Evidence: "e"}}
	named := gate.BannerFor("arrai-t41-gate-finish", flags)
	if !strings.HasPrefix(named, gate.BannerHeading) {
		t.Fatalf("heading no longer leads: %s", named)
	}
	first := strings.SplitN(named, "\n", 2)[0]
	if !strings.Contains(first, "Report from arrai-t41-gate-finish.") {
		t.Fatalf("author not on the heading line: %q", first)
	}
	if gate.BannerFor("", flags) != gate.Banner(flags) {
		t.Fatal("anonymous BannerFor diverged from Banner")
	}
	if gate.BannerFor("x", nil) != "" {
		t.Fatal("empty flags must stay silent")
	}
}

// bin/gate check, run from inside the citing repo, composes the same flag.
func TestT1027CheckCLIFlagsForeignGate(t *testing.T) {
	storeDir := t.TempDir()
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	jevons, _ := t1027Repo(t, "jevons")
	arrai, _ := t1027Repo(t, "arrai")
	rec := runGateIn(t, store, arrai, "arrai-gate")
	report := filepath.Join(jevons, "report.md")
	if err := os.WriteFile(report, []byte("Done.\n\n    "+rec.Attestation()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(dir string) (int, string) {
		cmd := exec.Command(gateBinary(t), "check", report)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+storeDir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if _, isExit := errAsExit(err); !isExit {
				t.Fatalf("running check: %v (%s)", err, out)
			}
		}
		return cmd.ProcessState.ExitCode(), string(out)
	}
	// The report lives in jevons, so gitRootNear resolves the citing repo
	// from its path whatever the process cwd is.
	code, out := run(t.TempDir())
	if code == 0 || !strings.Contains(out, string(gate.FlagForeignRepoGate)) {
		t.Fatalf("check did not flag the foreign gate (exit %d):\n%s", code, out)
	}
	// Control: the same report checked from inside the repo that ran the gate.
	own := filepath.Join(arrai, "report.md")
	if err := os.WriteFile(own, []byte("Done.\n\n    "+rec.Attestation()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(gateBinary(t), "check", own)
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+storeDir)
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "no false-green flags") {
		t.Fatalf("own-repo control flagged: %v\n%s", err, out)
	}
}
