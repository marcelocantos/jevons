package ownerquestion

import (
	"strings"
	"testing"
)

func TestExplicitOwnerBlockerIsAnswerableAndBounded(t *testing.T) {
	repo := t.TempDir()
	base := "```jevons\njevons: kind finish-report\njevons: target T77\njevons: status blocked\njevons: blocker owner decision requested: choose A or B for the landing page\njevons: oracle sha=abcdef0123456789\njevons: silent-ledger none\n```\n"
	q, ok, err := FromExplicitBlocker(base, repo, "worker")
	if err != nil || !ok || !strings.Contains(q.Text, "choose A or B") || !strings.Contains(q.Text, "commit abcdef") || q.AnswerRoute == "" {
		t.Fatalf("question %+v %v %v", q, ok, err)
	}
	again, ok, _ := FromExplicitBlocker(base, repo, "worker")
	if !ok || again.Identity != q.Identity {
		t.Fatal("replay changed identity")
	}
	revised, ok, _ := FromExplicitBlocker(strings.Replace(base, "choose A or B", "choose A or C", 1), repo, "worker")
	if !ok || revised.Identity.Version == q.Identity.Version || revised.Identity.ID != q.Identity.ID {
		t.Fatal("changed ask did not version")
	}
	for _, blocker := range []string{"Pixel Fold disconnected; owner verdict outstanding", "external device disconnected", "owner decision requested: wait for device", "owner review requested: now"} {
		raw := strings.Replace(base, "owner decision requested: choose A or B for the landing page", blocker, 1)
		if got, ok, _ := FromExplicitBlocker(raw, repo, "worker"); ok {
			t.Fatalf("promoted %q: %+v", blocker, got)
		}
	}
	noEvidence := strings.Replace(base, "jevons: oracle sha=abcdef0123456789\n", "", 1)
	if _, ok, _ := FromExplicitBlocker(noEvidence, repo, "worker"); ok {
		t.Fatal("missing evidence")
	}
}
