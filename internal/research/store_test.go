// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreApplyPersistsJSONAndMarkdown(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	note, delta, err := store.Apply(RevisionInput{
		Topic:    "repo/jevons",
		Title:    "Repository activity: jevons",
		Trigger:  "test",
		At:       at(0),
		Findings: []Finding{{Key: "scope:web", Claim: "light activity in web"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !delta.Changed || note.ID != "repo-jevons" {
		t.Fatalf("unexpected first apply: %+v %+v", note, delta)
	}
	if _, err := os.Stat(store.Path()); err != nil {
		t.Fatalf("canonical ledger missing: %v", err)
	}
	md, err := os.ReadFile(store.NotePath(note.ID))
	if err != nil {
		t.Fatalf("markdown render missing: %v", err)
	}
	if !strings.Contains(string(md), "light activity in web") {
		t.Fatalf("render missing claim:\n%s", md)
	}

	// Reopening must see the durable note (listable output surface).
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	notes, err := reopened.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 1 || notes[0].Topic != "repo/jevons" {
		t.Fatalf("want one durable note, got %+v", notes)
	}
	got, err := reopened.Get("repo/jevons")
	if err != nil {
		t.Fatalf("Get by topic: %v", err)
	}
	if got.ID != "repo-jevons" {
		t.Fatalf("Get returned wrong note: %+v", got)
	}
	if _, err := reopened.Get("repo/nope"); err == nil {
		t.Fatal("missing note must error")
	}
}

func TestStoreApplyUpdatesInPlaceAcrossTopics(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, topic := range []string{"repo/jevons", "context/frontier", "repo/jevons"} {
		if _, _, err := store.Apply(RevisionInput{
			Topic:    topic,
			Trigger:  "test",
			At:       at(0),
			Findings: []Finding{{Key: "k", Claim: "claim for " + topic}},
		}); err != nil {
			t.Fatalf("Apply %s: %v", topic, err)
		}
	}
	notes, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("repeat topic must update in place, got %d notes", len(notes))
	}
}

func TestStoreRejectsMalformedLedger(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.WriteFile(store.Path(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("malformed durable state must be a hard error, never a silent reset")
	}
}

func TestStoreApplyRequiresTopic(t *testing.T) {
	store, err := OpenDir(filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	if _, _, err := store.Apply(RevisionInput{Trigger: "test"}); err == nil {
		t.Fatal("apply without a topic must error")
	}
}
