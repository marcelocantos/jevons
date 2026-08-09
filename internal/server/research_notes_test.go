// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/research"
)

func researchNotesServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	state := t.TempDir()
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, state
}

func seedResearchNote(t *testing.T, state string) *research.Store {
	t.Helper()
	store, err := research.Open(state)
	if err != nil {
		t.Fatalf("research.Open: %v", err)
	}
	base := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	if _, _, err := store.Apply(research.RevisionInput{
		Topic:   "repo/jevons",
		Title:   "Repository activity: jevons",
		Trigger: "schedule",
		At:      base,
		Findings: []research.Finding{
			{Key: "scope:internal/research", Claim: "light activity in internal/research; head aaaa1111"},
		},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, _, err := store.Apply(research.RevisionInput{
		Topic:   "repo/jevons",
		Trigger: "feed:model-news",
		At:      base.Add(time.Hour),
		Findings: []research.Finding{
			{Key: "scope:internal/research", Claim: "heavy activity in internal/research; head bbbb2222"},
		},
	}); err != nil {
		t.Fatalf("Apply 2: %v", err)
	}
	return store
}

// 🎯T356 oracle: an empty store is an honest empty list, never an error.
func TestResearchNotesEmptyStoreIsHonest(t *testing.T) {
	srv, _ := researchNotesServer(t)
	resp, err := http.Get(srv.URL + "/api/research/notes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out researchNotesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 0 || out.Notes == nil || len(out.Notes) != 0 {
		t.Fatalf("empty store not honest: %+v", out)
	}
}

// 🎯T356 acceptance #4: notes are listable and readable off the product API.
func TestResearchNotesListAndRead(t *testing.T) {
	srv, state := researchNotesServer(t)
	seedResearchNote(t, state)

	resp, err := http.Get(srv.URL + "/api/research/notes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list researchNotesResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 {
		t.Fatalf("want one note, got %+v", list)
	}
	note := list.Notes[0]
	if note.ID != "repo-jevons" || note.Revisions != 2 || note.CurrentFindings != 1 {
		t.Fatalf("summary wrong: %+v", note)
	}
	if note.LastTrigger != "feed:model-news" {
		t.Fatalf("summary should carry the last trigger: %+v", note)
	}
	if !strings.HasSuffix(note.Path, "repo-jevons.md") {
		t.Fatalf("summary should carry the markdown path: %q", note.Path)
	}

	one, err := http.Get(srv.URL + "/api/research/notes/repo-jevons")
	if err != nil {
		t.Fatal(err)
	}
	defer one.Body.Close()
	if one.StatusCode != http.StatusOK {
		t.Fatalf("status %d", one.StatusCode)
	}
	var got researchNoteResponse
	if err := json.NewDecoder(one.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Note.Revisions) != 2 || len(got.Note.Findings) != 2 {
		t.Fatalf("full history must survive the wire: %+v", got.Note)
	}
	// The superseded conclusion is still readable — that is the whole point.
	if !strings.Contains(got.Markdown, "aaaa1111") || !strings.Contains(got.Markdown, "bbbb2222") {
		t.Fatalf("markdown should carry both conclusions:\n%s", got.Markdown)
	}
	sup := got.Note.Revisions[1].Superseded
	if len(sup) != 1 || sup[0].PriorRev != 1 {
		t.Fatalf("supersession provenance missing: %+v", sup)
	}
}

func TestResearchNoteMissingIsNotFound(t *testing.T) {
	srv, state := researchNotesServer(t)
	seedResearchNote(t, state)
	resp, err := http.Get(srv.URL + "/api/research/notes/repo-nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}
