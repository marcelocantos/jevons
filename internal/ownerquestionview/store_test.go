package ownerquestionview

import (
	"os"
	"path/filepath"
	"testing"
)

func question(repo, target, id, version string) Question {
	return Question{Identity: Identity{Repo: repo, Target: target, ID: id, Version: version}, Text: "Choose A or B?", Asker: "po", AnswerRoute: "reply to po"}
}
func TestCrossRepoBacklogAndLifecycleAfterRestart(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	root := t.TempDir()
	repos := []string{"jevons", "claudia", "bullseye", "spyder", "arrai", "yourworld2", "ge", "multimaze2"}
	for _, repo := range repos {
		if err := os.Mkdir(filepath.Join(root, repo), 0700); err != nil {
			t.Fatal(err)
		}
	}
	type row struct{ repo, target string }
	backlog := []row{{"jevons", "T967.1"}, {"jevons", "T766.3"}, {"jevons", "T1020"}, {"jevons", "T995"}, {"yourworld2", "T44.6"}, {"yourworld2", "T44.3"}, {"arrai", "T45"}}
	for _, r := range backlog {
		if err := s.Record(question(filepath.Join(root, r.repo), r.target, "owner-decision", "v1")); err != nil {
			t.Fatal(err)
		}
	}
	// Other repos' questions are not lost merely because no one named them in
	// the scout backlog; the set is global rather than a hardcoded allowlist.
	for _, repo := range repos {
		if err := s.Record(question(filepath.Join(root, repo), "T1", "design", "v1")); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []row{{"jevons", "T967.1"}, {"jevons", "T995"}, {"jevons", "T1020"}, {"arrai", "T45"}} {
		if err := s.Resolve(Identity{filepath.Join(root, r.repo), r.target, "owner-decision", "v1"}, Answered, "owner answered in chat; relayed to PO"); err != nil {
			t.Fatal(err)
		}
	}
	cold := New(dir)
	open, err := cold.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != len(repos)+3 {
		t.Fatalf("open=%d want %d: %+v", len(open), len(repos)+3, open)
	}
	all, err := cold.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(repos)+7 {
		t.Fatalf("all=%d", len(all))
	}
	for _, q := range all {
		if q.Identity.Target == "T45" && (q.Identity.Repo != filepath.Join(root, "arrai") || q.State != Answered) {
			t.Fatalf("T45 belongs to arrai and was answered: %+v", q)
		}
	}
	if err := cold.Record(question(filepath.Join(root, "jevons"), "T766.3", "owner-decision", "v2")); err != nil {
		t.Fatal(err)
	}
	if err := cold.Resolve(Identity{filepath.Join(root, "jevons"), "T766.3", "owner-decision", "v1"}, Answered, "late answer"); err == nil {
		t.Fatal("superseded version accepted late answer")
	}
	open, err = cold.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != len(repos)+3 {
		t.Fatalf("revision leaked duplicate open question: %d", len(open))
	}
}
func TestInvalidAndCorruptStoreAreNotSilentEmpty(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Record(question("relative", "T1", "q", "v1")); err == nil {
		t.Fatal("relative repo accepted")
	}
	if err := s.Resolve(Identity{"/repo", "T1", "q", "v1"}, Answered, "owner said yes"); err == nil {
		t.Fatal("unknown id resolved")
	}
	if err := os.WriteFile(filepath.Join(dir, "owner-questions.json"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir).List(true); err == nil {
		t.Fatal("corrupt store hidden")
	}
}
