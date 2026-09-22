// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/staffops"
)

// 🎯T814 — a sentinel file+PO notice for an unchanged symptom set is one
// delivery per recipient, with the saved turns counted; a changed set delivers.

func t814Server(t *testing.T, detail *string) (*Server, *[]string) {
	t.Helper()
	s := New(t.TempDir(), nil, nil)
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })
	s.SetCostMonitor(func() (*cost.Snapshot, error) {
		return &cost.Snapshot{
			Alerts: []cost.Alert{{Kind: "global-rate", Level: cost.LevelWarn, Detail: *detail}},
		}, nil
	})
	return s, &delivered
}

func t814Cycle(s *Server, at time.Time) (staffops.CycleResult, SentinelActResult) {
	return s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: s.deliveryStateDir(),
		Now: func() time.Time { return at },
	})
}

func TestT814IdenticalSentinelCyclesDeliverOnceAndCountSaved(t *testing.T) {
	detail := "hot"
	s, delivered := t814Server(t, &detail)
	start := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	now := start
	s.SetNoticeCoalesceClock(func() time.Time { return now })
	// The T428 overseer replay ledger has its own unconfirmed-delivery grace
	// (notifyReplayUnconfirmedGrace); it must ride the same virtual clock or
	// it suppresses the recurrence this test drives with simulated hours.
	s.SetNotifyReplayClock(func() time.Time { return now })

	const n = 4
	for i := range n {
		// Past the symptom cooldown each cycle, as the field repeats were.
		at := start.Add(time.Duration(i) * 2 * staffops.DefaultCooldown)
		now = at
		res, _ := t814Cycle(s, at)
		if res.Primary != staffops.ActionFilePO {
			t.Fatalf("cycle %d primary=%s want file+PO\n%s", i, res.Primary, res.WireText)
		}
	}
	// One PO notice plus one overseer notice, however many cycles repeat.
	if len(*delivered) != 2 {
		t.Fatalf("deliveries=%d want 2 (PO + overseer) for %d identical cycles\n%v", len(*delivered), n, *delivered)
	}
	saved := s.NoticeSavedTurns()
	for _, k := range []string{sentinelNoticeKey(defaultProductPOName), sentinelNoticeKey(s.overseerName())} {
		if saved[k] != n-1 {
			t.Fatalf("saved[%s]=%d want %d (all=%v)", k, saved[k], n-1, saved)
		}
	}
}

func TestT814ChangedSymptomSetDeliversAgain(t *testing.T) {
	detail := "hot"
	s, delivered := t814Server(t, &detail)
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	s.SetNoticeCoalesceClock(func() time.Time { return now })

	t814Cycle(s, now)
	now = now.Add(2 * staffops.DefaultCooldown)
	detail = "hotter: a different symptom set"
	t814Cycle(s, now)
	if len(*delivered) != 4 {
		t.Fatalf("deliveries=%d want 4 (2 recipients x 2 distinct sets)\n%v", len(*delivered), *delivered)
	}
}

// t814Repo is a workdir whose history mentions target T814 in a commit.
func t814Repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "track: fold ledger writes (🎯T814 filing)"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"),
		[]byte("schema_version: \"1.0\"\ntargets:\n  T814:\n    name: x\n    status: identified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func t814Verdict(t *testing.T, dir, report string) staffops.IdleResidueVerdict {
	t.Helper()
	if _, err := agentreport.Save(dir, "jv-x", report, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ao := staffops.AgentObs{Name: "jv-x", IdleResidue: true, OpenMission: true}
	fillIdleResidueEvidence(&ao, claudia.AgentDef{Name: "jv-x", TargetID: "T814", WorkDir: dir}, dir, fleetintent.Snapshot{})
	if !ao.HasBoundCommits {
		t.Fatalf("fixture must carry a commit mentioning the target: %+v", ao)
	}
	return staffops.ClassifyIdleResidue(staffops.IdleResidueEvidence{
		IdleResidue: true, OpenMission: true, BoundTarget: ao.BoundTarget,
		HasBoundCommits: ao.HasBoundCommits, TargetLedgerStatus: ao.TargetLedgerStatus,
		ReportLooksFinished: ao.ReportLooksFinished,
		OwnerAskPresent:     ao.OwnerAskPresent,
	})
}

func TestT814InProgressReportWithMentioningCommitIsNotFinished(t *testing.T) {
	dir := t814Repo(t)
	v := t814Verdict(t, dir, "Still in progress: the classifier edit is written, tests are running.")
	if v.Class == staffops.IdleResidueFinishedAwaitingGate {
		t.Fatalf("in-progress newest report must not classify finished: %+v", v)
	}
}

func TestT814TerminalReportStillNotifies(t *testing.T) {
	dir := t814Repo(t)
	v := t814Verdict(t, dir, "🎯T814 done. Commit abcdef1 lands the classifier.\n`go test ./internal/staffops/ -run T814` PASS.")
	if v.Class != staffops.IdleResidueFinishedAwaitingGate {
		t.Fatalf("terminal report must classify finished: %+v", v)
	}
}

func TestT814ClearedConditionDeliversAgain(t *testing.T) {
	detail := "hot"
	s, delivered := t814Server(t, &detail)
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	s.SetNoticeCoalesceClock(func() time.Time { return now })
	// The T428 overseer replay ledger's own unconfirmed-delivery grace must
	// ride this test's simulated clock too, or the recurrence this test
	// drives past the T814 clear window still looks like "0s ago" to it and
	// gets suppressed as an unconfirmed replay.
	s.SetNotifyReplayClock(func() time.Time { return now })

	t814Cycle(s, now)
	now = now.Add(SentinelNoticeClearedAfter + time.Hour) // silent past the clear window
	t814Cycle(s, now)
	if len(*delivered) != 4 {
		t.Fatalf("deliveries=%d want 4 (recurrence after clearing)\n%v", len(*delivered), *delivered)
	}
}
