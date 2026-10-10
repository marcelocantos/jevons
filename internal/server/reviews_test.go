package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

func TestReviewIndexIdentityLifecycleAndFailClosedAnswer(t *testing.T) {
	dir := t.TempDir()
	store := ownerquestionview.New(dir)
	mk := func(repo, version, id string) ownerquestionview.Question {
		return ownerquestionview.Question{Identity: ownerquestionview.Identity{Repo: repo, Target: "T1", ID: id, Version: version}, Text: "May I proceed?", Asker: "po", AnswerRoute: "jevons_owner_gate op=answer"}
	}
	a := mk(t.TempDir(), "v1", "owner-gate")
	b := mk(t.TempDir(), "v1", "owner-gate")
	for _, q := range []ownerquestionview.Question{a, b} {
		if err := store.Record(q); err != nil {
			t.Fatal(err)
		}
	}
	if reviewID(a.Identity) == reviewID(b.Identity) {
		t.Fatal("cross-repository collision")
	}
	s := New("test", dir)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	w := get("/api/reviews")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var list []reviewItem
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want two repos, got %+v", list)
	}
	if list[0].Readiness != "actionable" {
		t.Fatal(list[0])
	}
	w = get("/api/reviews/" + reviewID(a.Identity))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var exact reviewItem
	if err := json.Unmarshal(w.Body.Bytes(), &exact); err != nil {
		t.Fatal(err)
	}
	if exact.Identity != a.Identity || exact.URL != "/review/"+reviewID(a.Identity) {
		t.Fatal(exact)
	}
	v2 := mk(a.Identity.Repo, "v2", "owner-gate")
	if err := store.Record(v2); err != nil {
		t.Fatal(err)
	}
	w = get("/api/reviews/" + reviewID(a.Identity))
	if err := json.Unmarshal(w.Body.Bytes(), &exact); err != nil {
		t.Fatal(err)
	}
	if exact.State != ownerquestionview.Superseded || exact.Readiness != "closed" {
		t.Fatal(exact)
	}
	w = get("/api/reviews")
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("closed item appeared in open list: %+v", list)
	}
	blocked := mk(a.Identity.Repo, "v1", "device-approval")
	if err := store.Record(blocked); err != nil {
		t.Fatal(err)
	}
	w = get("/api/reviews/" + reviewID(blocked.Identity))
	if err := json.Unmarshal(w.Body.Bytes(), &exact); err != nil {
		t.Fatal(err)
	}
	if exact.Readiness != "prerequisite_blocked" {
		t.Fatal(exact)
	}
	for _, id := range []string{reviewID(v2.Identity), reviewID(a.Identity), reviewID(blocked.Identity), "unknown"} {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/reviews/"+id+"/answer", strings.NewReader(`{"expected_version":"v2","action":"approve"}`)))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d %s", id, w.Code, w.Body.String())
		}
	}
	rows, err := store.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatal(rows)
	}
}
