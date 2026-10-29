// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// An open question whose target the ledger has already achieved is not a
// decision for the owner (🎯T1122): the default list omits it, while
// include_closed and the by-id route still return it.
func TestReviewListOmitsQuestionsOnClosedTargets(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	repo, _ = ownerquestion.SharedRepo(repo)
	ledger := filepath.Join(repo, "ledger.fixture")
	if err := os.WriteFile(ledger, []byte("targets:\n  T1:\n    name: done one\n    status: achieved\n  T2:\n    name: open one\n    status: identified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := runBullseyeCLI
	runBullseyeCLI = func(args ...string) (string, error) { return "File: " + ledger + "\n", nil }
	t.Cleanup(func() { runBullseyeCLI = old })

	state := t.TempDir()
	qs := ownerquestionview.New(state)
	stale := ownerquestionview.Question{Identity: ownerquestionview.Identity{Repo: repo, Target: "T1", ID: "q", Version: "v1"}, Text: "Still?", Asker: "po", AnswerRoute: "chat"}
	live := ownerquestionview.Question{Identity: ownerquestionview.Identity{Repo: repo, Target: "T2", ID: "q", Version: "v1"}, Text: "Proceed?", Asker: "po", AnswerRoute: "chat"}
	for _, q := range []ownerquestionview.Question{stale, live} {
		if err := qs.Record(q); err != nil {
			t.Fatal(err)
		}
	}
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	list := func(path string) []reviewItem {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var out []reviewItem
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := list("/api/reviews")
	if len(got) != 1 || got[0].Target != "T2" {
		t.Fatalf("default list: want only T2, got %+v", got)
	}
	if all := list("/api/reviews?include_closed=true"); len(all) != 2 {
		t.Fatalf("include_closed: want both, got %+v", all)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/reviews/"+reviewID(stale.Identity), nil))
	if w.Code != 200 {
		t.Fatalf("by id: %d %s", w.Code, w.Body.String())
	}
}
