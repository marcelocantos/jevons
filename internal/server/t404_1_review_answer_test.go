// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

func TestT4041ReviewAnswerRequiresOwnerSession(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	s.SetOwnerQuestionAnswerHook(func(id ownerquestionview.Identity, answer string) (string, error) {
		t.Fatal("hook must not run without owner session")
		return "", nil
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/reviews/probe/answer", strings.NewReader(`{"answer":"x"}`))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.SetPathValue("id", "probe")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestT4041ReviewAnswerRoutesThroughHook(t *testing.T) {
	dir := t.TempDir()
	store := ownerquestionview.New(dir)
	repo := t.TempDir()
	q := ownerquestionview.Question{
		Identity: ownerquestionview.Identity{Repo: repo, Target: "T404.1", ID: "surface", Version: "1"},
		Text:     "Cockpit or Slack?",
		Asker:    "jv-t404-auto",
		AnswerRoute: "jv-t404-auto",
		Default:  "cockpit",
		Options:  []string{"cockpit", "slack"},
	}
	if err := store.Record(q); err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	id := reviewID(q.Identity)
	var got ownerquestionview.Identity
	s.SetOwnerQuestionAnswerHook(func(identity ownerquestionview.Identity, answer string) (string, error) {
		got = identity
		if answer != "slack" {
			t.Fatalf("answer=%q", answer)
		}
		return "routed: answered to jv-t404-auto (sent)", nil
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	req, rr := browserOwnerPOST(s, "/api/reviews/"+id+"/answer", `{"answer":"slack"}`)
	req.SetPathValue("id", id)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "answered" || !strings.Contains(resp["routed"], "jv-t404-auto") {
		t.Fatalf("resp=%v", resp)
	}
	if got.Target != "T404.1" || got.ID != "surface" {
		t.Fatalf("identity=%+v", got)
	}
}

