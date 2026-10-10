package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

func TestReviewEvidenceVersionScopeAndHonestMissing(t *testing.T) {
	repo := t.TempDir()
	state := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %v %s", args, e, b)
		}
		return strings.TrimSpace(string(b))
	}
	run(repo, "init", "-q")
	repo, _ = ownerquestion.SharedRepo(repo)
	run(repo, "config", "user.email", "test@example.com")
	run(repo, "config", "user.name", "Test")
	if e := os.WriteFile(filepath.Join(repo, "example.txt"), []byte("hello\n"), 0600); e != nil {
		t.Fatal(e)
	}
	run(repo, "add", "example.txt")
	run(repo, "commit", "-qm", "example")
	sha := run(repo, "rev-parse", "HEAD")
	ledger := filepath.Join(repo, "ledger.fixture")
	if e := os.WriteFile(ledger, []byte("targets:\n  T766.3:\n    name: Review target title\n    status: converging\n"), 0600); e != nil {
		t.Fatal(e)
	}
	old := runBullseyeCLI
	runBullseyeCLI = func(args ...string) (string, error) { return "File: " + ledger + "\n", nil }
	t.Cleanup(func() { runBullseyeCLI = old })
	t.Setenv(gate.StoreDirEnv, t.TempDir())
	store, _ := gate.OpenStore("")
	own := &gate.Record{ID: "abcdef12", Tree: &gate.TreeProvenance{Repo: repo, Commit: sha}, Verdict: gate.VerdictGreen, StatusKnown: true}
	if e := store.Save(own); e != nil {
		t.Fatal(e)
	}
	foreign := &gate.Record{ID: "fedcba98", Tree: &gate.TreeProvenance{Repo: t.TempDir(), Commit: sha}, Verdict: gate.VerdictGreen, StatusKnown: true}
	if e := store.Save(foreign); e != nil {
		t.Fatal(e)
	}
	rec, e := agentreport.Save(state, "worker", "```jevons\njevons: target T766.3\n```", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	mk := func(version, evidence string) ownerquestionview.Question {
		id := ownerquestionview.Identity{Repo: repo, Target: "T766.3", ID: "scope", Version: version}
		return ownerquestionview.Question{Identity: id, Text: "What is the verdict?", Asker: "po", AnswerRoute: "chat", Review: &ownerquestion.ReviewEvent{Identity: ownerquestion.Identity(id), Readiness: ownerquestion.Actionable, Action: "Decide", Evidence: evidence}}
	}
	v1 := mk("v1", "commit "+sha+"; GATE test exit=0 GREEN id=abcdef12; report handle=worker/"+rec.ID+"; artifacts/screens/one.png")
	v2 := mk("v2", "commit "+strings.Repeat("a", 40)+"; GATE test exit=0 GREEN id=fedcba98; artifacts/../../etc/shadow.png")
	qs := ownerquestionview.New(state)
	if e := qs.Record(v1); e != nil {
		t.Fatal(e)
	}
	if e := qs.Record(v2); e != nil {
		t.Fatal(e)
	}
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	read := func(path string) (reviewItem, string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var x reviewItem
		if e := json.Unmarshal(w.Body.Bytes(), &x); e != nil {
			t.Fatal(e)
		}
		return x, w.Body.String()
	}
	a, raw := read("/api/reviews/" + reviewID(v1.Identity))
	if a.SchemaVersion != 2 || a.TargetLookup != "verified" || a.TargetTitle != "Review target title" || a.State != ownerquestionview.Superseded || a.Readiness != "closed" || a.Evidence.Commit.Status != "verified" || a.Evidence.Gate.Status != "verified" || a.Evidence.Diff.URL == "" || a.Evidence.Report.URL == "" || len(a.Evidence.Screenshots) != 1 || a.Evidence.Screenshots[0].URL != "" {
		t.Fatal(a)
	}
	if strings.Contains(raw, repo) {
		t.Fatal("absolute repo disclosed", raw)
	}
	b, _ := read("/api/reviews/" + reviewID(v2.Identity))
	if b.Evidence.Commit.Status != "inaccessible" || b.Evidence.Gate.Status != "inaccessible" || b.Evidence.Diff.URL != "" || b.Evidence.Screenshots[0].URL != "" {
		t.Fatal(b)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", a.Evidence.Diff.URL, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "hello") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", a.Evidence.Report.URL, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "T766.3") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/reviews/../../etc/passwd/diff", nil))
	if w.Code == 200 {
		t.Fatal("traversal served")
	}
}
