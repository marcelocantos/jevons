// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/attrib"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/seatload"
)

const (
	t734Worker = "jv-t717-replay-digest"
	t734Parent = "jevons-po"
	t734Target = "T717"
)

func TestT734CommitMentionsTarget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text, target string
		want         bool
	}{
		{"fix(T717): leftover pane", "T717", true},
		{"fix(🎯T717): leftover pane", "T717", true},
		{"fix(T717): leftover pane", "🎯T717", true},
		{"mentions T717 in the body", "T717", true},
		{"fix(T7170): longer id", "T717", false},
		{"fix(T717.1): dotted child", "T717", false},
		{"fix(T71): shorter id", "T717", false},
		{"fix(T999): other leaf", "T717", false},
		{"no target at all", "T717", false},
		{"", "T717", false},
		{"fix(T717): leftover", "", false},
	}
	for _, tc := range cases {
		if got := commitMentionsTarget(tc.text, tc.target); got != tc.want {
			t.Errorf("commitMentionsTarget(%q, %q) = %v, want %v", tc.text, tc.target, got, tc.want)
		}
	}
}

func TestT734FormatNoticeNamesSeatCommitsAndTarget(t *testing.T) {
	t.Parallel()
	got := FormatPostReapCommitNotice(t734Worker, t734Target, "", []gitCommit{
		{SHA: "21ac010cb066deadbeef", Subject: "fix(T717): MCP, T530, frontier, and dead-agent recurrences differ on the wire"},
	}, false)
	for _, want := range []string{
		postReapCommitPrefix,
		t734Worker,
		"21ac010cb066",
		"🎯T717",
		"no longer covers the tree",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notice missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Live successor") {
		t.Errorf("quiet successor clause leaked:\n%s", got)
	}

	withSucc := FormatPostReapCommitNotice(t734Worker, t734Target, "jv-t717-next", []gitCommit{
		{SHA: "21ac010cb066deadbeef", Subject: "fix(T717): leftover"},
	}, false)

	unattributed := FormatPostReapCommitNotice(t734Worker, t734Target, "", []gitCommit{
		{SHA: "21ac010cb066deadbeef", Subject: "fix(T717): leftover"},
	}, true)
	if !strings.Contains(unattributed, "unattributed commit") {
		t.Errorf("unattributed notice missing label:\n%s", unattributed)
	}
	if strings.Contains(unattributed, "reaped seat") {
		t.Errorf("unattributed notice blamed the watch seat:\n%s", unattributed)
	}
	if !strings.Contains(withSucc, "Live successor jv-t717-next") {
		t.Errorf("successor not attributed:\n%s", withSucc)
	}
}

func TestT734FilterDropsUnrelatedAndEmpty(t *testing.T) {
	t.Parallel()
	commits := []gitCommit{
		{SHA: "aaa", Subject: "fix(T717): leftover pane"},
		{SHA: "bbb", Subject: "fix(T999): adjacent leaf"},
	}
	hits := FilterPostReapCommits(commits, "T717")
	if len(hits) != 1 || hits[0].SHA != "aaa" {
		t.Fatalf("hits = %+v, want only the T717 commit", hits)
	}
	if got := FilterPostReapCommits(commits, ""); len(got) != 0 {
		t.Fatalf("empty target kept %v", got)
	}
	if got := FilterPostReapCommits(nil, "T717"); len(got) != 0 {
		t.Fatalf("empty input kept %v", got)
	}
}

func TestT734ReapedSeatCommitRaisesNotice(t *testing.T) {
	s, parent, repo := t734Server(t)
	t734Reap(t, s, t734Worker)

	sha := t734CommitAttributed(t, repo, "leftover.go", "still going\n", "fix(T717): MCP recurrences differ on the wire", t734Worker)
	s.SweepPostReapCommits()
	if len(parent.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1; got %v", len(parent.sent), parent.sent)
	}
	got := parent.sent[0]
	for _, want := range []string{postReapCommitPrefix, t734Worker, sha, "🎯T717"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice missing %q:\n%s", want, got)
		}
	}

	s.SweepPostReapCommits()
	if len(parent.sent) != 1 {
		t.Fatalf("second sweep re-notified: %v", parent.sent)
	}
}

func TestT734QuietReapProducesNoNotice(t *testing.T) {
	s, parent, _ := t734Server(t)
	t734Reap(t, s, t734Worker)
	s.SweepPostReapCommits()
	if len(parent.sent) != 0 {
		t.Fatalf("quiet reap produced a notice: %v", parent.sent)
	}
}

func TestT734UnrelatedCommitProducesNoNotice(t *testing.T) {
	s, parent, repo := t734Server(t)
	t734Reap(t, s, t734Worker)
	t734Commit(t, repo, "other.go", "adjacent\n", "fix(T999): next leaf on the frontier")
	s.SweepPostReapCommits()
	if len(parent.sent) != 0 {
		t.Fatalf("unrelated commit attributed to the reaped seat: %v", parent.sent)
	}
}

func TestT734ReapDoneCommitRaisesNotice(t *testing.T) {
	s, parent, repo := t734Server(t)
	ok, err := s.RemovalAccount().Remove(s.registry, t734Worker, fleetlog.Removal{
		Reason: fleetlog.ReasonReapDone,
		Detail: "reaped on a finished-work report (finished_work)",
	})
	if err != nil || !ok {
		t.Fatalf("reap: ok=%v err=%v", ok, err)
	}
	sha := t734CommitAttributed(t, repo, "leftover.go", "still going\n", "fix(T717): finish-report leftover", t734Worker)
	s.SweepPostReapCommits()
	if len(parent.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1; got %v", len(parent.sent), parent.sent)
	}
	if !strings.Contains(parent.sent[0], sha) {
		t.Fatalf("T165 reap notice missing %s:\n%s", sha, parent.sent[0])
	}
}

func TestT734KillDoesNotArmWatch(t *testing.T) {
	s, parent, repo := t734Server(t)
	ok, err := s.RemovalAccount().Remove(s.registry, t734Worker, fleetlog.Removal{
		Reason: fleetlog.ReasonKill,
		Detail: "killed by explicit request",
	})
	if err != nil || !ok {
		t.Fatalf("kill: ok=%v err=%v", ok, err)
	}
	t734Commit(t, repo, "leftover.go", "still going\n", "fix(T717): should not notify on kill")
	s.SweepPostReapCommits()
	if len(parent.sent) != 0 {
		t.Fatalf("kill path raised a post-reap notice: %v", parent.sent)
	}
}

func TestT734DroppingTheNoticeGoesRed(t *testing.T) {
	s, parent, repo := t734Server(t)
	t734Reap(t, s, t734Worker)
	sha := t734CommitAttributed(t, repo, "leftover.go", "still going\n", "fix(T717): leftover pane", t734Worker)

	s.mu.Lock()
	head := s.postReapWatches[t734Worker].Head
	s.mu.Unlock()
	commits, err := listCommitsAfter(repo, head)
	if err != nil {
		t.Fatal(err)
	}
	if hits, _ := SelectPostReapNoticeCommits(commits, postReapWatch{Seat: t734Worker, TargetID: t734Target}); len(hits) == 0 {
		t.Fatal("mutation: SelectPostReapNoticeCommits dropped the leftover T717 commit")
	}

	s.SweepPostReapCommits()
	if len(parent.sent) == 0 {
		t.Fatalf("mutation: sweep dropped the parent notice for %s", sha)
	}
	if !strings.Contains(parent.sent[0], t734Worker) || !strings.Contains(parent.sent[0], sha) {
		t.Fatalf("mutation: notice lost the seat or the commit:\n%s", parent.sent[0])
	}
}

func t734Server(t *testing.T) (*Server, *fakeSender, string) {
	t.Helper()
	repo := t734Repo(t)
	parent := &fakeSender{alive: true}
	s, _ := chainServer(t, map[string]*fakeSender{t734Parent: parent})
	dir := t.TempDir()
	s.stateDir = dir
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: t734Parent, WorkDir: repo, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons"},
		{Name: t734Worker, WorkDir: repo, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: t734Parent, TargetID: t734Target},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s.registry = reg
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	s.SetRemovalAccount(fleetlog.New(nil))
	s.RemovalAccount().SetBeforeRemoveHook(s.onAccountedRemoving)
	s.RemovalAccount().SetRemovedHook(s.onAccountedRemoval)
	return s, parent, repo
}

// 🎯T708-on-reap: reap_done / reap_achieve must take detached load with the
// seat, same as explicit kill. A quiet root is not signalled — that would
// be killing the in-flight write T734 chose not to destroy.
func TestT734ReapPathReapsDetachedLoad(t *testing.T) {
	for _, reason := range []string{fleetlog.ReasonReapDone, fleetlog.ReasonReapAchieve} {
		t.Run(reason, func(t *testing.T) {
			var signalled []int
			s, _, _ := t734Server(t)
			s.seatLoad = &seatload.Tracker{
				List: func() (seatload.Table, error) {
					return seatload.Table{
						{PID: 1, PPID: 0, PGID: 1, Command: "/sbin/launchd"},
						{PID: 500, PPID: 1, PGID: 500, Command: "jevonsd"},
						{PID: 900, PPID: 500, PGID: 900, Command: "grok --seat " + t734Worker},
						{PID: 902, PPID: 1, PGID: 900, CPUPercent: 190, Command: "/bin/sh -c while :; do go test -race ./...; done"},
					}, nil
				},
				Signal: func(pid int, _ syscall.Signal) error { signalled = append(signalled, pid); return nil },
				Sleep:  func(time.Duration) {},
				Self:   500,
			}
			if _, err := s.seatLoad.Track(t734Worker, 900); err != nil {
				t.Fatal(err)
			}
			ok, err := s.RemovalAccount().Remove(s.registry, t734Worker, fleetlog.Removal{Reason: reason})
			if err != nil || !ok {
				t.Fatalf("remove: ok=%v err=%v", ok, err)
			}
			if len(signalled) == 0 {
				t.Fatalf("%s left the detached loop running", reason)
			}
			for _, pid := range signalled {
				if pid == 500 || pid == 900 || pid <= 1 {
					t.Fatalf("%s signalled seat root or daemon %d", reason, pid)
				}
			}
		})
	}
}

func t734Reap(t *testing.T, s *Server, name string) {
	t.Helper()
	ok, err := s.RemovalAccount().Remove(s.registry, name, fleetlog.Removal{
		Reason: fleetlog.ReasonReapAchieve,
		Detail: "reaped on achieve of 🎯T717",
	})
	if err != nil || !ok {
		t.Fatalf("reap %s: ok=%v err=%v", name, ok, err)
	}
	if _, reaped := LookupReapedRecord(s.fleetIntent(), name); !reaped {
		t.Fatal("reaped intent missing")
	}
	s.mu.Lock()
	_, armed := s.postReapWatches[name]
	s.mu.Unlock()
	if !armed {
		t.Fatal("reap did not arm a post-reap commit watch")
	}
}

func t734Repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := attrib.Git(dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "master")
	git("config", "user.email", "t734@test")
	git("config", "user.name", "t734")
	if err := os.WriteFile(filepath.Join(dir, "base.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "base.go")
	git("-c", "core.hooksPath=/dev/null", "commit", "-q", "--no-verify", "-m", "base")
	return dir
}

func t734Commit(t *testing.T, repo, rel, content, message string) string {
	t.Helper()
	return t734CommitAttributed(t, repo, rel, content, message, "")
}

func t734CommitAttributed(t *testing.T, repo, rel, content, message, actor string) string {
	t.Helper()
	if actor != "" {
		message += "\n\nJevons-Actor: " + actor + "\n"
	}
	p := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := attrib.Git(repo, "add", rel); err != nil {
		t.Fatalf("add %s: %v\n%s", rel, err, out)
	}
	if out, err := attrib.Git(repo, "-c", "core.hooksPath=/dev/null", "commit", "-q", "--no-verify", "-m", message); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	sha, err := attrib.Git(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(sha)
}

func TestT760LedgerOnlyCommitAfterReapStaysSilent(t *testing.T) {
	s, parent, repo := t734Server(t)
	t734Reap(t, s, t734Worker)
	t734CommitAttributed(t, repo, "bullseye.yaml", "targets:\n  T98:\n    status: achieved\n", "🎯T98: achieve ledger-only (mentions T717 closure)", t734Parent)
	s.SweepPostReapCommits()
	if len(parent.sent) != 0 {
		t.Fatalf("ledger-only PO commit blamed the reaped seat: %v", parent.sent)
	}
}

func TestT760OtherActorProductCommitStaysSilent(t *testing.T) {
	s, parent, repo := t734Server(t)
	t734Reap(t, s, t734Worker)
	t734CommitAttributed(t, repo, "other.go", "adjacent\n", "fix(T717): attributed to a live seat", t734Parent)
	s.SweepPostReapCommits()
	if len(parent.sent) != 0 {
		t.Fatalf("another actor's commit was blamed on the reaped seat: %v", parent.sent)
	}
}

func TestT760UnattributedProductCommitDoesNotBlameReapedSeat(t *testing.T) {
	s, parent, repo := t734Server(t)
	t734Reap(t, s, t734Worker)
	sha := t734Commit(t, repo, "leftover.go", "still going\n", "fix(T717): no actor trailer")
	s.SweepPostReapCommits()
	if len(parent.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1; got %v", len(parent.sent), parent.sent)
	}
	got := parent.sent[0]
	if !strings.Contains(got, "unattributed commit") {
		t.Fatalf("notice should say unattributed:\n%s", got)
	}
	if strings.Contains(got, "reaped seat "+t734Worker) {
		t.Fatalf("notice blamed the watch seat without provenance:\n%s", got)
	}
	if !strings.Contains(got, sha) {
		t.Fatalf("notice missing commit %s:\n%s", sha, got)
	}
}

func TestT760SelectPostReapNoticeCommits(t *testing.T) {
	t.Parallel()
	watch := postReapWatch{Seat: t734Worker, TargetID: t734Target}
	commits := []gitCommit{
		{SHA: "a", Subject: "fix(T717): ledger", Files: []string{"bullseye.yaml"}},
		{SHA: "b", Subject: "fix(T717): po product", Actor: t734Parent, Files: []string{"other.go"}},
		{SHA: "c", Subject: "fix(T717): worker", Actor: t734Worker, Files: []string{"leftover.go"}},
		{SHA: "d", Subject: "fix(T717): unknown", Files: []string{"leftover.go"}},
	}
	got, unattributed := SelectPostReapNoticeCommits(commits, watch)
	if len(got) != 2 {
		t.Fatalf("got %d commits, want worker + unattributed product", len(got))
	}
	if got[0].SHA != "c" || got[1].SHA != "d" {
		t.Fatalf("wrong commits kept: %+v", got)
	}
	if !unattributed {
		t.Fatal("expected unattributed flag")
	}
}
