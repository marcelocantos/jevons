// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T721 — the scout-to-implement handoff is not a re-brief.

func t721ScoutReport(target string) string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindScoutReport,
		Target:       target,
		Phase:        envelope.PhaseScout,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Ready for an implement brief; no reslice.",
	})
}

func t721ImplementBrief(target string) string {
	return envelope.Format(&envelope.Message{
		Kind:    envelope.KindSpawnBrief,
		Target:  target,
		Phase:   envelope.PhaseImplement,
		Payload: "Build from the scout ledger.",
	})
}

func t721SaveReport(t *testing.T, s *Server, name, text string) {
	t.Helper()
	dir := t.TempDir()
	if _, err := agentreport.Save(dir, name, text, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SetAgentReportDir(dir)
}

func TestT721ScoutReportThenImplementBriefIsDelivered(t *testing.T) {
	const name = "jv-t718-gate-dirty-warn"
	s, sender, workdir := t597Fixture(t, name)

	// Workdir touch would have refused under T597 alone (the specimen also
	// had bullseye.yaml mtime). The scout-report is the discriminator.
	if err := os.WriteFile(filepath.Join(workdir, "pofanout.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t721SaveReport(t, s, name, t721ScoutReport("T718"))

	sendRes := t597Send(t, s, name, t721ImplementBrief("T718"), false)
	if sendRes.IsError {
		t.Fatalf("phase-implement after same-target scout-report must deliver: %s", toolText(sendRes))
	}
	if len(sender.sent) != 1 {
		t.Fatalf("handoff must reach the seat once: %v", sender.sent)
	}
}

func TestT721BuriedScoutReportFenceStillCounts(t *testing.T) {
	const name = "jv-t718-buried"
	s, sender, _ := t597Fixture(t, name)

	buried := "Scout phase for 🎯T718: thinking before the fence.\n\n" +
		t721ScoutReport("T718") +
		"\nReady for an implement brief; no reslice.\n"
	if parseTerminalEnvelope(buried) == nil || parseTerminalEnvelope(buried).Kind != envelope.KindScoutReport {
		t.Fatalf("buried fence must parse as scout-report:\n%s", buried)
	}
	t721SaveReport(t, s, name, buried)

	sendRes := t597Send(t, s, name, t721ImplementBrief("🎯T718"), false)
	if sendRes.IsError {
		t.Fatalf("buried scout-report must still hand off: %s", toolText(sendRes))
	}
	if len(sender.sent) != 1 {
		t.Fatalf("handoff must reach the seat: %v", sender.sent)
	}
}

func TestT721ScoutReportDifferentTargetRefused(t *testing.T) {
	const name = "jv-t718-other-target"
	s, sender, _ := t597Fixture(t, name)
	t721SaveReport(t, s, name, t721ScoutReport("T999"))

	sendRes := t597Send(t, s, name, t721ImplementBrief("T718"), false)
	if !sendRes.IsError {
		t.Fatalf("scout-report for a different target must refuse: %s", toolText(sendRes))
	}
	refusal := toolText(sendRes)
	if !strings.Contains(refusal, "re-brief refused") || !strings.Contains(refusal, "force_rebrief=true") {
		t.Fatalf("refusal missing override: %s", refusal)
	}
	if strings.Contains(refusal, "restart the mission") {
		t.Fatalf("refusal must not tell the caller to restart: %s", refusal)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("refused brief must not reach the seat: %v", sender.sent)
	}
}

func TestT721NoScoutReportStillRefused(t *testing.T) {
	const name = "jv-t718-checkpoint"
	s, sender, _ := t597Fixture(t, name)
	t721SaveReport(t, s, name, "checkpoint: mechanism established")

	sendRes := t597Send(t, s, name, t721ImplementBrief("T718"), false)
	if !sendRes.IsError {
		t.Fatalf("checkpoint report is not a scout-report: %s", toolText(sendRes))
	}
	if len(sender.sent) != 0 {
		t.Fatalf("refused brief must not reach the seat: %v", sender.sent)
	}
}

func TestT721PhaseScoutSpawnBriefStillRefused(t *testing.T) {
	const name = "jv-t718-rescout"
	s, sender, _ := t597Fixture(t, name)
	t721SaveReport(t, s, name, t721ScoutReport("T718"))

	rescout := envelope.Format(&envelope.Message{
		Kind:    envelope.KindSpawnBrief,
		Target:  "T718",
		Phase:   envelope.PhaseScout,
		Payload: "Scout again.",
	})
	sendRes := t597Send(t, s, name, rescout, false)
	if !sendRes.IsError {
		t.Fatalf("phase scout to a working seat is a re-brief: %s", toolText(sendRes))
	}
	if len(sender.sent) != 0 {
		t.Fatalf("refused brief must not reach the seat: %v", sender.sent)
	}
}

func TestT721PhaseAdvanceHandoff(t *testing.T) {
	scout := t721ScoutReport("T718")
	impl := t721ImplementBrief("T718")
	implEmoji := t721ImplementBrief("🎯T718")
	other := t721ImplementBrief("T999")
	checkpoint := "checkpoint: mechanism established"
	omitted := "```jevons\njevons: kind spawn-brief\njevons: target T718\n```\n\nBuild.\n"

	cases := []struct {
		name     string
		incoming string
		latest   string
		want     bool
	}{
		{"same target", impl, scout, true},
		{"emoji target", implEmoji, scout, true},
		{"omitted phase defaults implement", omitted, scout, true},
		{"buried scout", impl, "thinking\n\n" + scout + "\nmore\n", true},
		{"other target", other, scout, false},
		{"checkpoint latest", impl, checkpoint, false},
		{"empty latest", impl, "", false},
		{"phase scout incoming", envelope.Format(&envelope.Message{
			Kind: envelope.KindSpawnBrief, Target: "T718", Phase: envelope.PhaseScout, Payload: "x",
		}), scout, false},
	}
	for _, c := range cases {
		if got := phaseAdvanceHandoff(c.incoming, c.latest); got != c.want {
			t.Errorf("%s: phaseAdvanceHandoff=%v want %v", c.name, got, c.want)
		}
	}
}

func TestT721AdmitEveryBriefMutantGoesRed(t *testing.T) {
	// Over-broad mutant: every spawn-brief is a phase advance. That admits
	// t597SpawnBrief onto a working seat and would go green on the T597
	// workdir tape — this helper staying false is what keeps that tape RED
	// for the mutant.
	mutant := func(incoming, latest string) bool { return true }
	if !mutant(t597SpawnBrief, "checkpoint: mechanism established") {
		t.Fatal("mutant setup")
	}
	if phaseAdvanceHandoff(t597SpawnBrief, "checkpoint: mechanism established") {
		t.Fatal("phaseAdvanceHandoff admitted a checkpoint report — the admit-every-brief mutant is live")
	}
	if phaseAdvanceHandoff(t597SpawnBrief, t721ScoutReport("T718")) {
		t.Fatal("phaseAdvanceHandoff matched T999 implement against a T718 scout-report")
	}
}
