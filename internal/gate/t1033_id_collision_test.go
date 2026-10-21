// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 🎯T1033 — a gate run never silently overwrites an existing record.
//
// newID yields 8 hex chars from sha256(argv|unixnano|pid). Store.Save used
// write-and-rename with no existence check, so a colliding id replaced
// another repo's record and its attestation then resolved to the wrong run.
//
// This is the same construction and the same defect 🎯T749 fixed in
// internal/delivery (and 🎯T746 in internal/agentreport), carried here so
// the shared ~/.jevons/gates store cannot silently retarget a citation.
//
// THE GUARANTEE THIS SERVES: a GATE line's id= resolves to the run that
// printed it. A store that overwrites on collision does not make that
// guarantee — the surviving record may be a different repo's run.

// t1033At is one fixed instant. Every record in these tests is stamped
// with it, so the id collision is not a matter of test timing luck.
var t1033At = time.Date(2026, 10, 7, 15, 11, 53, 0, time.UTC)

func t1033Store(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return store
}

func t1033Run(t *testing.T, store *Store, name string) *Record {
	t.Helper()
	rec, err := Run(&RunArgs{
		Command: []string{"true"},
		Name:    name,
		Dir:     t.TempDir(), // not a git work tree: GREEN, not DIRTY
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Now:     func() time.Time { return t1033At },
	})
	if err != nil {
		t.Fatalf("Run(%s): %v", name, err)
	}
	return rec
}

func t1033CountJSON(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			n++
		}
	}
	return n
}

// recordOverwriting is the pre-🎯T1033 store path, kept in its original
// shape: write a temp file beside the target and rename over whatever is
// there.
//
// It is here so the mutation is demonstrated rather than asserted. The
// tests below run it and the product path over identical input and show
// this one losing a record — so if Save is ever reverted to this
// behaviour, the assertions in this file stop passing for a reason the
// file itself explains.
func recordOverwriting(t *testing.T, dir string, rec *Record) {
	t.Helper()
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := writeAtomic(filepath.Join(dir, rec.ID+".json"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
}

// Acceptance: Run refuses to reuse an id that already names a record
// (re-derives / lengthens) and two records saved with a forced collision
// both survive with distinct ids. The first keeps the historical 8-hex
// proposal so ids already on disk still resolve.
func TestT1033ForcedCollisionBothSurvive(t *testing.T) {
	argv := []string{"true"}
	legacyID := newID(argv, t1033At)

	// Control: the legacy path, on the id the product path would propose.
	legacyDir := t.TempDir()
	a := &Record{ID: legacyID, Name: "repo-arrai", Command: argv, StatusKnown: true, Verdict: VerdictGreen}
	b := &Record{ID: legacyID, Name: "repo-jevons", Command: argv, StatusKnown: true, Verdict: VerdictGreen}
	recordOverwriting(t, legacyDir, a)
	recordOverwriting(t, legacyDir, b)
	if n := t1033CountJSON(t, legacyDir); n != 1 {
		t.Fatalf("control: legacy path wrote %d files, want 1 — the mutation is not the mutation", n)
	}

	store := t1033Store(t)
	first := t1033Run(t, store, "repo-arrai")
	second := t1033Run(t, store, "repo-jevons")
	if first.ID == second.ID {
		t.Fatalf("both records got id %q — the second overwrote the first", first.ID)
	}
	if first.ID != legacyID {
		t.Fatalf("first record got id %q, want the unchanged base id %q — ids already on disk must still resolve", first.ID, legacyID)
	}
	if len(first.ID) != idWidth {
		t.Fatalf("base id %q is %d hex, want %d", first.ID, len(first.ID), idWidth)
	}
	if len(second.ID) != collideWidth {
		t.Fatalf("colliding id %q is %d hex, want lengthened %d", second.ID, len(second.ID), collideWidth)
	}

	if n := t1033CountJSON(t, store.Root); n != 2 {
		t.Fatalf("product path kept %d of 2 records — the overwrite is back", n)
	}
	gotFirst, ok, err := store.Load(first.ID)
	if err != nil || !ok {
		t.Fatalf("Load(%s) ok=%v err=%v", first.ID, ok, err)
	}
	gotSecond, ok, err := store.Load(second.ID)
	if err != nil || !ok {
		t.Fatalf("Load(%s) ok=%v err=%v", second.ID, ok, err)
	}
	if gotFirst.Name != "repo-arrai" {
		t.Fatalf("first record came back as %+v", gotFirst)
	}
	if gotSecond.Name != "repo-jevons" {
		t.Fatalf("second record came back as %+v", gotSecond)
	}
	if gotFirst.ID != first.ID || gotSecond.ID != second.ID {
		t.Fatalf("stored id disagrees with the filename: %q/%q vs %q/%q", gotFirst.ID, gotSecond.ID, first.ID, second.ID)
	}
}

// Cited attestations continue to resolve to the run that printed them.
func TestT1033CitedAttestationsResolveToTheRunThatPrintedThem(t *testing.T) {
	store := t1033Store(t)
	first := t1033Run(t, store, "repo-arrai")
	second := t1033Run(t, store, "repo-jevons")

	report := "🎯T1033 done; both gates green.\n\n    " +
		first.Attestation() + "\n    " +
		second.Attestation() + "\n"
	cited := ParseAttestations(report)
	if len(cited) != 2 {
		t.Fatalf("ParseAttestations found %d, want 2 in:\n%s", len(cited), report)
	}
	if cited[0].ID != first.ID || cited[0].Name != first.Name {
		t.Fatalf("first citation = %+v, want id=%s name=%s", cited[0], first.ID, first.Name)
	}
	if cited[1].ID != second.ID || cited[1].Name != second.Name {
		t.Fatalf("second citation = %+v, want id=%s name=%s", cited[1], second.ID, second.Name)
	}

	printed := map[string]string{first.ID: first.Attestation(), second.ID: second.Attestation()}
	for _, c := range cited {
		rec, ok := store.Lookup(c.ID)
		if !ok {
			t.Fatalf("cited id %q is not retrievable", c.ID)
		}
		if rec.Name != c.Name {
			t.Fatalf("id %q resolved to name %q, the citation said %q — the overwrite is back", c.ID, rec.Name, c.Name)
		}
		if got, want := rec.Attestation(), printed[c.ID]; got != want {
			t.Fatalf("id %q attestation drifted: stored %q vs printed %q", c.ID, got, want)
		}
	}

	// A report citing both as green is not a false-green: each id really
	// is the run that printed it.
	if flags := FlagFalseGreen(report, store.Lookup); len(flags) != 0 {
		t.Fatalf("honest dual citation flagged: %v", flags)
	}
}

// Save itself refuses an overwrite: a direct colliding Save is an error,
// and the first record is still the one Load returns.
func TestT1033SaveRefusesOverwrite(t *testing.T) {
	store := t1033Store(t)
	first := &Record{
		ID: "c0ffee01", Name: "repo-arrai", Command: []string{"true"},
		StatusKnown: true, Verdict: VerdictGreen, Started: t1033At,
	}
	if err := store.Save(first); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	second := &Record{
		ID: "c0ffee01", Name: "repo-jevons", Command: []string{"true"},
		StatusKnown: true, Verdict: VerdictGreen, Started: t1033At,
	}
	err := store.Save(second)
	if !errors.Is(err, ErrIDCollision) {
		t.Fatalf("colliding Save err=%v, want ErrIDCollision", err)
	}
	got, ok, err := store.Load("c0ffee01")
	if err != nil || !ok {
		t.Fatalf("Load after refused overwrite: ok=%v err=%v", ok, err)
	}
	if got.Name != "repo-arrai" {
		t.Fatalf("first record was replaced: %+v", got)
	}
	if n := t1033CountJSON(t, store.Root); n != 1 {
		t.Fatalf("store holds %d records after a refused overwrite, want 1", n)
	}
}

// The mutation goes RED: restore the overwrite and a record is lost.
func TestT1033MutationRestoringTheOverwriteLosesARecord(t *testing.T) {
	dir := t.TempDir()
	id := newID([]string{"true"}, t1033At)
	recordOverwriting(t, dir, &Record{ID: id, Name: "repo-arrai", StatusKnown: true, Verdict: VerdictGreen})
	recordOverwriting(t, dir, &Record{ID: id, Name: "repo-jevons", StatusKnown: true, Verdict: VerdictGreen})
	if n := t1033CountJSON(t, dir); n != 1 {
		t.Fatalf("legacy path wrote %d files, want 1", n)
	}
	blob, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatalf("read survivor: %v", err)
	}
	var survivor Record
	if err := json.Unmarshal(blob, &survivor); err != nil {
		t.Fatalf("parse survivor: %v", err)
	}
	if survivor.Name != "repo-jevons" {
		t.Fatalf("survivor name=%q", survivor.Name)
	}
	// The "repo-arrai" record is gone, with nothing naming it — which is
	// the silence this target ends.
	if _, err := os.Stat(filepath.Join(dir, id+"n1.json")); err == nil {
		t.Fatal("legacy path somehow produced a sibling — the control is not the control")
	}
}

// Concurrent records racing for one id all survive, and none is left
// holding another's bytes.
func TestT1033ConcurrentCollisionAllSurvive(t *testing.T) {
	const n = 16
	store := t1033Store(t)
	dir := t.TempDir()

	var wg sync.WaitGroup
	results := make([]*Record, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = Run(&RunArgs{
				Command: []string{"true"},
				Name:    fmt.Sprintf("racer-%02d", i),
				Dir:     dir,
				Store:   store,
				Stdout:  io.Discard,
				Stderr:  io.Discard,
				Now:     func() time.Time { return t1033At },
			})
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
		if seen[results[i].ID] {
			t.Fatalf("racer %d got duplicate id %q", i, results[i].ID)
		}
		seen[results[i].ID] = true
	}
	if got := t1033CountJSON(t, store.Root); got != n {
		t.Fatalf("%d of %d concurrent records survived", got, n)
	}
	names := map[string]bool{}
	for id := range seen {
		rec, ok, err := store.Load(id)
		if err != nil || !ok {
			t.Fatalf("Load(%s) ok=%v err=%v", id, ok, err)
		}
		if rec.ID != id {
			t.Fatalf("file %s.json stores id %q — the body was published under a name that is not its id", id, rec.ID)
		}
		if names[rec.Name] {
			t.Fatalf("name %q appears in two files — temp files interleaved", rec.Name)
		}
		names[rec.Name] = true
	}
	if len(names) != n {
		t.Fatalf("%d distinct names across %d files", len(names), n)
	}
	entries, err := os.ReadDir(store.Root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".gate-") {
			t.Fatalf("temp file %s survived the write", e.Name())
		}
	}
}

// A record that does not collide mints exactly the id the old scheme minted.
func TestT1033BaseIDIsUnchanged(t *testing.T) {
	store := t1033Store(t)
	want := newID([]string{"true"}, t1033At)
	rec := t1033Run(t, store, "solo")
	if rec.ID != want {
		t.Fatalf("non-colliding record got id %q, want %q", rec.ID, want)
	}
	if !ValidRecordID(rec.ID) {
		t.Fatalf("proposed id %q is not a legal record handle", rec.ID)
	}
}
