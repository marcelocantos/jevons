// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T725 tapes. A workdir probe that treats bullseye.yaml as this seat's
// activity inverts who is implementing: the PO files targets in a shared
// clone, every seat lights up, and the refusal names a file the seat
// never opened. The second half of the acceptance is the load-bearing
// one — a seat that actually edited source is still detected.

func TestT725SharedLedgerIsNotSeatActivity(t *testing.T) {
	const name = "jv-t725-idle-in-shared-clone"
	s, sender, workdir := t597Fixture(t, name)

	// Another actor (the PO) just wrote the ledger. This seat touched
	// nothing of its own.
	if err := os.WriteFile(filepath.Join(workdir, "bullseye.yaml"), []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	act := s.seatActivity(name)
	if act.Active() {
		t.Fatalf("idle seat in a clone where bullseye.yaml was just written must not be active: %s", act.Describe())
	}
	if !act.SharedSkipped {
		t.Fatal("probe must record that a shared file was seen and excluded")
	}
	got := act.Describe()
	if citedWorkdirFile(got, "bullseye.yaml") {
		t.Fatalf("must not cite a workdir file the seat may never have opened:\n%s", got)
	}
	if !strings.Contains(got, sharedFileExclusion) {
		t.Fatalf("inactive verdict must say shared files are excluded, not name them as this seat's touch:\n%s", got)
	}

	res := callTranscriptRead(t, s, name)
	text := toolText(res)
	if strings.Contains(text, "Seat activity: ACTIVE") {
		t.Fatalf("shared ledger write must not classify ACTIVE:\n%s", text)
	}
	if citedWorkdirFile(text, "bullseye.yaml") {
		t.Fatalf("transcript_read must not name bullseye.yaml as this seat's touch:\n%s", text)
	}

	sendRes := t597Send(t, s, name, t597SpawnBrief, false)
	if sendRes.IsError {
		t.Fatalf("brief to a seat with only a shared ledger touch must be accepted: %s", toolText(sendRes))
	}
	if len(sender.sent) != 1 {
		t.Fatalf("brief must reach the idle seat: %v", sender.sent)
	}
}

func TestT725SourceEditStillDetected(t *testing.T) {
	const name = "jv-t725-real-worker"
	s, sender, workdir := t597Fixture(t, name)

	// Shared clone: the ledger was just written, AND this seat edited
	// source. WalkDir hits bullseye.yaml first (lexical); the probe
	// must skip it and still find the source file.
	if err := os.WriteFile(filepath.Join(workdir, "bullseye.yaml"), []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "pofanout.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	act := s.seatActivity(name)
	if !act.Active() {
		t.Fatalf("a seat whose own source edits are on disk must be active: %s", act.Describe())
	}
	got := act.Describe()
	if !strings.Contains(got, "pofanout.go") {
		t.Fatalf("must cite the source edit, not go silent:\n%s", got)
	}
	if citedWorkdirFile(got, "bullseye.yaml") {
		t.Fatalf("must not cite the shared ledger as this seat's touch:\n%s", got)
	}
	if !act.SharedSkipped || !strings.Contains(got, sharedFileExclusion) {
		t.Fatalf("refusal-path describe must say shared files are excluded:\n%s", got)
	}

	res := callTranscriptRead(t, s, name)
	text := toolText(res)
	if res.IsError || !strings.Contains(text, "ACTIVE") || !strings.Contains(text, "pofanout.go") {
		t.Fatalf("source-edit seat must read ACTIVE citing pofanout.go:\n%s", text)
	}

	sendRes := t597Send(t, s, name, t597SpawnBrief, false)
	if !sendRes.IsError {
		t.Fatalf("spawn-brief to a seat that edited source must be refused: %s", toolText(sendRes))
	}
	refusal := toolText(sendRes)
	if !strings.Contains(refusal, "pofanout.go") {
		t.Fatalf("refusal must name the source file:\n%s", refusal)
	}
	if citedWorkdirFile(refusal, "bullseye.yaml") {
		t.Fatalf("refusal must not name the shared ledger:\n%s", refusal)
	}
	if !strings.Contains(refusal, sharedFileExclusion) {
		t.Fatalf("refusal must say shared files are excluded rather than naming them:\n%s", refusal)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("refused re-brief must not reach the seat: %v", sender.sent)
	}
}

func TestT725StoredReportDoesNotCiteSharedLedger(t *testing.T) {
	// The T718 specimen: a stored report plus a bullseye.yaml mtime that
	// belonged to the PO. The report is this seat's activity; the ledger
	// write is not.
	const name = "jv-t725-report-plus-ledger"
	s, sender, workdir := t597Fixture(t, name)
	stateDir := t.TempDir()
	if _, err := agentreport.Save(stateDir, name, "checkpoint: mechanism established", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SetAgentReportDir(stateDir)
	if err := os.WriteFile(filepath.Join(workdir, "bullseye.yaml"), []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sendRes := t597Send(t, s, name, t597SpawnBrief, false)
	if !sendRes.IsError {
		t.Fatalf("stored report is still activity: %s", toolText(sendRes))
	}
	refusal := toolText(sendRes)
	if !strings.Contains(refusal, "stored report") {
		t.Fatalf("refusal must cite the stored report:\n%s", refusal)
	}
	if citedWorkdirFile(refusal, "bullseye.yaml") {
		t.Fatalf("refusal must not name the shared ledger:\n%s", refusal)
	}
	if !strings.Contains(refusal, sharedFileExclusion) {
		t.Fatalf("refusal must say shared files are excluded rather than naming them:\n%s", refusal)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("refused re-brief must not reach the seat: %v", sender.sent)
	}
}

func TestT725BareMtimeMutantGoesRed(t *testing.T) {
	// Over-broad mutant: every regular file counts, including the
	// shared ledger. That is the pre-T725 walk, and it is what named
	// bullseye.yaml as jv-t718-gate-dirty-warn's activity.
	mutant := func(path string) bool { return true }
	if !mutant("bullseye.yaml") {
		t.Fatal("mutant setup")
	}
	if workdirTouchCounts("bullseye.yaml") {
		t.Fatal("workdirTouchCounts accepted bullseye.yaml — the bare-mtime-on-any-file mutant is live")
	}
	if workdirTouchCounts("/clone/bullseye.yaml") {
		t.Fatal("workdirTouchCounts accepted a rooted bullseye.yaml")
	}
	if !workdirTouchCounts("pofanout.go") {
		t.Fatal("source files must still count — over-excluding makes every seat read idle")
	}
	if !workdirTouchCounts("internal/gate/run.go") {
		t.Fatal("nested source must still count")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, _, skipped := latestWorkdirTouch(dir, time.Now().Add(-time.Minute))
	if path != "" {
		t.Fatalf("latestWorkdirTouch cited %q for a ledger-only tree", path)
	}
	if !skipped {
		t.Fatal("latestWorkdirTouch must report the shared file was seen and skipped")
	}

	if err := os.WriteFile(filepath.Join(dir, "pofanout.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, _, skipped = latestWorkdirTouch(dir, time.Now().Add(-time.Minute))
	if path == "" || !strings.HasSuffix(path, "pofanout.go") {
		t.Fatalf("source edit in the same tree must still be found, got %q", path)
	}
	if !skipped {
		t.Fatal("shared file in the same tree must still be recorded as excluded")
	}
}

func TestT725WorkdirTouchCounts(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"bullseye.yaml", false},
		{"Bullseye.yaml", false},
		{"/repo/bullseye.yaml", false},
		{"pofanout.go", true},
		{"internal/mcpserver/t597_briefless.go", true},
		{"docs/design/frontier-as-ready-set.md", true},
		{"go.mod", true},
		{"AGENTS.md", true},
	}
	for _, c := range cases {
		if got := workdirTouchCounts(c.path); got != c.want {
			t.Errorf("workdirTouchCounts(%q)=%v want %v", c.path, got, c.want)
		}
	}
}

// citedWorkdirFile reports whether text names path as a "workdir file"
// evidence clause — not a mere mention of the basename in the exclusion
// note.
func citedWorkdirFile(text, base string) bool {
	idx := strings.Index(text, "workdir file ")
	for idx >= 0 {
		rest := text[idx+len("workdir file "):]
		end := strings.IndexAny(rest, " ;")
		if end < 0 {
			end = len(rest)
		}
		cited := rest[:end]
		if strings.HasSuffix(cited, base) || strings.Contains(cited, "/"+base) {
			return true
		}
		next := strings.Index(rest, "workdir file ")
		if next < 0 {
			return false
		}
		idx += len("workdir file ") + next
	}
	return false
}
