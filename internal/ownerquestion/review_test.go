package ownerquestion

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

func TestT1041ActualBlockedReportAndNegativeControls(t *testing.T) {
	raw, err := os.ReadFile("testdata/t1041-blocked-report.txt")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	e, ok, err := FromActionableReview(string(raw), repo, "reviewer")
	if err != nil || !ok || e.Identity.Target != "T1041" || e.Identity.ID != "hardware-visual-review" || e.Identity.Repo != mustCanonical(t, repo) {
		t.Fatalf("actual fixture not admitted: %+v %v %v", e, ok, err)
	}
	for _, want := range []string{"Reconnect the Fold", "folded/unfolded", "accept/reject", "72d1b163", "jevons-po", "artifacts/t1041-development-390.png"} {
		if !strings.Contains(e.Question().Text, want) {
			t.Errorf("missing %s", want)
		}
	}
	replay, ok, err := FromActionableReview(string(raw), repo, "reviewer")
	if err != nil || !ok || replay.Identity != e.Identity {
		t.Fatalf("unstable identity: %+v", replay)
	}
	for name, alter := range map[string]func(string) string{
		"device only":       func(s string) string { return strings.ReplaceAll(s, "and owner verdict", "") },
		"no screenshot":     func(s string) string { return strings.ReplaceAll(s, "Screenshots:", "Images:") },
		"no activation":     func(s string) string { return strings.ReplaceAll(s, "reported serving", "reported built") },
		"no visual verdict": func(s string) string { return strings.ReplaceAll(s, "Visual verdict", "check") },
		"no hardware":       func(s string) string { return strings.ReplaceAll(s, "folded/unfolded", "device") },
		"no commit": func(s string) string {
			return strings.ReplaceAll(s, "sha=72d1b16368c74eb4dfc9a1407023835ff6db8221", "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, _ := FromActionableReview(alter(string(raw)), repo, "reviewer")
			if ok {
				t.Fatal("generic block promoted")
			}
		})
	}
	m := &envelope.Message{Kind: envelope.KindFinishReport, Target: "T1", Status: envelope.ProgressBlocked, Blocker: "owner go-ahead on restart", SilentLedger: envelope.SilentLedgerEmpty}
	if _, ok, _ := FromActionableReview(envelope.Format(m), repo, "worker"); ok {
		t.Fatal("bare go-ahead promoted")
	}
	if _, ok, _ := FromActionableReview("quoted report:\n"+string(raw), repo, "worker"); ok {
		t.Fatal("quoted report promoted")
	}
}

func TestSharedRepoWorktree(t *testing.T) {
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	out, err := SharedRepo(wd)
	if err != nil {
		t.Fatal(err)
	}
	common := filepath.Clean(strings.TrimSpace(string(mustGit(t, "rev-parse", "--path-format=absolute", "--git-common-dir"))))
	if filepath.Base(common) == ".git" && out != filepath.Dir(common) {
		t.Fatalf("got %s want %s", out, filepath.Dir(common))
	}
}
func mustGit(t *testing.T, args ...string) []byte {
	t.Helper()
	b, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustCanonical(t *testing.T, dir string) string {
	t.Helper()
	root, err := CanonicalRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
