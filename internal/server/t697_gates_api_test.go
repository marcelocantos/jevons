// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T697: a cited GATE id is readable over the served path. The test writes
// a known record into a hermetic store, resolves it through GET /api/gates/{id}
// on the registered mux (not by calling the handler with a hand-set path),
// and asserts command / exit / verdict / tree round-trip. An unknown id
// returns distinguishable not-found rather than anything a report could
// cite as a pass.
func TestT697GateAPIRoundTripAndNotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv(gate.StoreDirEnv, root)
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	rec := &gate.Record{
		ID:          "c2b2e23b",
		Name:        "make-test-go",
		Command:     []string{"make", "test-go"},
		Dir:         "/tmp/jevons",
		Started:     time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC),
		Ended:       time.Date(2026, 9, 20, 20, 0, 42, 0, time.UTC),
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     gate.VerdictGreen,
		Tree: &gate.TreeProvenance{
			Repo:       "/tmp/jevons",
			Commit:     "ac753cbbdeadbeef0123456789abcdef",
			Clean:      false,
			DirtyFiles: 4,
			DirtySample: []string{
				"internal/server/gates.go",
			},
		},
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/gates/" + rec.ID)
	if err != nil {
		t.Fatalf("GET known id: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET known id status=%d body=%s", resp.StatusCode, body)
	}
	var view gate.View
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode known: %v", err)
	}
	if !view.Found {
		t.Fatal("known record returned found=false")
	}
	if view.Status != "0" {
		t.Fatalf("status=%q want 0", view.Status)
	}
	if view.Verdict != gate.VerdictGreen {
		t.Fatalf("verdict=%s want GREEN", view.Verdict)
	}
	if len(view.Command) != 2 || view.Command[0] != "make" || view.Command[1] != "test-go" {
		t.Fatalf("command=%#v", view.Command)
	}
	if view.Tree == nil {
		t.Fatal("tree missing")
	}
	if view.Tree.Clean || view.Tree.DirtyFiles != 4 || view.Tree.Commit != rec.Tree.Commit {
		t.Fatalf("tree=%+v", view.Tree)
	}
	if view.ExitStatus != 0 || !view.StatusKnown || view.ID != rec.ID || view.Name != rec.Name {
		t.Fatalf("view=%+v", view)
	}

	unknown := "932855c1"
	nf, err := http.Get(srv.URL + "/api/gates/" + unknown)
	if err != nil {
		t.Fatalf("GET unknown id: %v", err)
	}
	defer nf.Body.Close()
	if nf.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(nf.Body)
		t.Fatalf("unknown status=%d body=%s, want 404", nf.StatusCode, body)
	}
	var miss gate.NotFoundView
	if err := json.NewDecoder(nf.Body).Decode(&miss); err != nil {
		t.Fatalf("decode unknown: %v", err)
	}
	if miss.Error != gate.ErrNotFound || miss.Found || miss.ID != unknown {
		t.Fatalf("not-found body=%+v — a supervisor must be able to tell this from a pass", miss)
	}
	if miss.Error == "GREEN" || miss.Error == "0" {
		t.Fatal("not-found body looks like a pass")
	}

	// Path-shaped ids must not walk the store; they are not-found, not a 500.
	esc, err := http.Get(srv.URL + "/api/gates/..%2fetc")
	if err != nil {
		t.Fatalf("GET traversal: %v", err)
	}
	defer esc.Body.Close()
	if esc.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(esc.Body)
		t.Fatalf("traversal status=%d body=%s, want 404", esc.StatusCode, body)
	}
	var escBody gate.NotFoundView
	if err := json.NewDecoder(esc.Body).Decode(&escBody); err != nil {
		t.Fatalf("decode traversal: %v", err)
	}
	if escBody.Error != gate.ErrNotFound || escBody.Found {
		t.Fatalf("traversal body=%+v", escBody)
	}
}
