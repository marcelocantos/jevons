package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/targetfile"
)

func TestT753FinishReapHoldsLandedTargetAcrossRestart(t *testing.T) {
	s, parent, repo := t734Server(t)
	if err := s.OpenFleetIntent(s.stateDir); err != nil {
		t.Fatal(err)
	}
	sha := t734Commit(t, repo, "feature.go", "package p\n", "fix(T717): implementation")
	if err := os.WriteFile(filepath.Join(repo, "bullseye.yaml"), []byte("targets:\n  T717:\n    name: Small repair\n    status: identified\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.maybeReapDoneWorkAgent(t734Worker, "Done.")
	if s.registry.Def(t734Worker) != nil {
		t.Fatal("finish did not reap worker")
	}
	if len(parent.sent) != 1 || !strings.Contains(parent.sent[0], sha) || !strings.Contains(parent.sent[0], t734Parent) {
		t.Fatalf("missing owed decision: %v", parent.sent)
	}
	if err := s.OpenFleetIntent(s.stateDir); err != nil {
		t.Fatal(err)
	}
	spawned := 0
	args := FrontierConsumeLoopArgs{Workdir: repo, Spawn: func(targetfile.FrontierLeaf, string, string) error { spawned++; return nil }}
	reps := s.frontierConsumeSweep(args, nil)
	if spawned != 0 || len(reps) != 1 || reps[0].Reason != "reaped_target_pending" {
		t.Fatalf("duplicate work: spawned=%d reports=%+v", spawned, reps)
	}
	if note := s.reapedTargetPending("T717", t.TempDir(), false); note != "" {
		t.Fatal("hold leaked to another repo")
	}
	if note := s.reapedTargetPending("T717", repo, true); note != "" {
		t.Fatal("explicit reopen failed")
	}
	s.frontierConsumeSweep(args, nil)
	if spawned != 1 {
		t.Fatalf("reopened target not spawned: %d", spawned)
	}
}

func TestT753NoImplementationDoesNotHold(t *testing.T) {
	for _, message := range []string{"fix(T7170): different target", "fix(T717.1): child", "ledger T717"} {
		t.Run(message, func(t *testing.T) {
			s, parent, repo := t734Server(t)
			if err := s.OpenFleetIntent(s.stateDir); err != nil {
				t.Fatal(err)
			}
			path := "other.go"
			if strings.HasPrefix(message, "ledger") {
				path = "bullseye.yaml"
			}
			t734Commit(t, repo, path, "# unrelated\n", message)
			s.maybeReapDoneWorkAgent(t734Worker, "Done.")
			if s.registry.Def(t734Worker) != nil {
				t.Fatal("did not reap")
			}
			if len(s.reapVerify.List()) != 0 || len(parent.sent) != 0 {
				t.Fatal("unstarted work held")
			}
		})
	}
}

func TestT753FailedPersistenceRetainsSeat(t *testing.T) {
	s, _, repo := t734Server(t)
	if err := s.OpenFleetIntent(s.stateDir); err != nil {
		t.Fatal(err)
	}
	t734Commit(t, repo, "feature.go", "package p\n", "fix(T717): implementation")
	if err := os.Mkdir(filepath.Join(s.stateDir, "fleet", "reap-verify.json.tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	s.maybeReapDoneWorkAgent(t734Worker, "Done.")
	if s.registry.Def(t734Worker) == nil {
		t.Fatal("removed seat without durable hold")
	}
	if len(s.reapVerify.List()) != 0 {
		t.Fatal("failed write changed memory")
	}
}

func TestT753ExistingForceEngageDoesNotReleaseNewHold(t *testing.T) {
	s, _, repo := t734Server(t)
	if err := s.OpenFleetIntent(s.stateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "bullseye.yaml"), []byte("targets:\n  T717:\n    name: Repair\n    status: identified\n    tags: [force-engage]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t734Commit(t, repo, "feature.go", "package p\n", "fix(T717): implementation")
	s.maybeReapDoneWorkAgent(t734Worker, "Done.")
	if note := s.reapedTargetPending("T717", repo, true); note == "" {
		t.Fatal("old force-engage tag released new hold")
	}
	if note := s.reapedTargetPending("T717", repo, false); note == "" {
		t.Fatal("removing tag released hold")
	}
	if note := s.reapedTargetPending("T717", repo, true); note != "" {
		t.Fatal("new explicit engagement failed")
	}
}
