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
	own := &gate.Record{ID: "abcdef12", Tree: &gate.TreeProvenance{Repo: repo, Commit: sha, Clean: true}, Verdict: gate.VerdictGreen, StatusKnown: true}
	if e := store.Save(own); e != nil {
		t.Fatal(e)
	}
	wrongCommit := &gate.Record{ID: "aaaabbbb", Tree: &gate.TreeProvenance{Repo: repo, Commit: strings.Repeat("b", 40), Clean: true}, Verdict: gate.VerdictGreen, StatusKnown: true}
	dirty := &gate.Record{ID: "ccccdddd", Tree: &gate.TreeProvenance{Repo: repo, Commit: sha, Clean: false}, Verdict: gate.VerdictGreen, StatusKnown: true}
	red := &gate.Record{ID: "eeeeffff", Tree: &gate.TreeProvenance{Repo: repo, Commit: sha, Clean: true}, Verdict: gate.VerdictRed, StatusKnown: true, ExitStatus: 1}
	for _, r := range []*gate.Record{wrongCommit, dirty, red} {
		if e := store.Save(r); e != nil {
			t.Fatal(e)
		}
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
	if a.SchemaVersion != 2 || a.TargetLookup != "verified" || a.TargetTitle != "Review target title" || a.State != ownerquestionview.Superseded || a.Readiness != "closed" || a.Evidence.Commit.Status != "verified" || a.Evidence.Gate.Status != "verified" || a.Evidence.Diff.Status != "reported_only" || a.Evidence.Diff.URL != "" || a.Evidence.Report.Status != "reported_only" || a.Evidence.Report.URL != "" || len(a.Evidence.Screenshots) != 1 || a.Evidence.Screenshots[0].URL != "" {
		t.Fatal(a)
	}
	if strings.Contains(raw, repo) {
		t.Fatal("absolute repo disclosed", raw)
	}
	b, _ := read("/api/reviews/" + reviewID(v2.Identity))
	if b.Evidence.Commit.Status != "reported_only" || b.Evidence.Gate.Status != "inaccessible" || b.Evidence.Diff.URL != "" || b.Evidence.Screenshots[0].URL != "" {
		t.Fatal(b)
	}
	w := httptest.NewRecorder()
	for _, suffix := range []string{"/diff", "/report", "/gate"} {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", a.URL+suffix, nil))
		if w.Code != 404 {
			t.Fatalf("raw evidence endpoint %s unexpectedly exposed: %d", suffix, w.Code)
		}
	}

	// A second SHA or gate in one claim is ambiguous; no arbitrary "first" pair.
	for _, tc := range []struct{ label, evidence, gateStatus string }{
		{"wrong-commit", "commit " + sha + " gate-id=aaaabbbb", "inaccessible"},
		{"dirty", "commit " + sha + " gate-id=ccccdddd", "inaccessible"},
		{"red", "commit " + sha + " gate-id=eeeeffff", "inaccessible"},
		{"ambiguous-sha", "commit " + sha + " commit " + strings.Repeat("c", 40) + " gate-id=abcdef12", "reported_only"},
	} {
		q := mk("v3-"+tc.label, tc.evidence)
		result := s.reviewDetail(q)
		if result.Evidence.Commit.Status == "verified" || result.Evidence.Gate.Status == "verified" || result.Evidence.Diff.URL != "" || result.Evidence.Gate.URL != "" {
			t.Fatalf("%s false verification: %+v", tc.label, result.Evidence)
		}
	}
	// A stored report body or git diff may contain secrets; these routes are
	// absent, even when the item has an otherwise verified commit and gate.
	for _, text := range []string{`file="/tmp/private"`, `--config=/tmp/secret`, `https://example.test/?token=/tmp/secret`, `token=/tmp/secret`, `/opt/unknown/secret`} {
		got := redactReviewPaths("Review "+text, repo)
		if strings.Contains(got, "/tmp/") || strings.Contains(got, "/opt/") || strings.Contains(got, "secret") || strings.Contains(got, "private") {
			t.Fatalf("path leak %q => %q", text, got)
		}
	}
	// No local filesystem, arbitrary report, or unverified citation may be
	// converted to a public URL through a revised or malicious record.
	corrupt := mk("v4", "commit "+sha+" gate-id=abcdef12")
	corrupt.Review.Identity.Version = "v-other"
	if item := s.reviewDetail(corrupt); item.Evidence.Commit.Status == "verified" {
		t.Fatal("mismatched review verified")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/reviews/../../etc/passwd/diff", nil))
	if w.Code == 200 {
		t.Fatal("traversal served")
	}
}
