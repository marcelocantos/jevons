// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/converge"
)

const (
	t761Worker       = "jv-t761-finished-not-nudged"
	t761FinishReport = "```jevons\njevons: kind finish-report\njevons: target T761\njevons: silent-ledger none\njevons: sha abcdef0123456\njevons: gate-id eb9e5245\n```\n" +
		"Oracle: bin/gate -- go test ./internal/mcpserver/ -run T761"
)

func TestT761StoredTerminalKindDoesNotRequireCompletionEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"finish missing oracle", t761FinishReport, true},
		{"scout", "```jevons\njevons: kind scout-report\njevons: target T761\njevons: silent-ledger none\n```", true},
		{"status", "```jevons\njevons: kind status-ping\njevons: status in-progress\n```", false},
		{"quoted finish", "Quoted example:\n" + t761FinishReport, false},
		{"prose", "Still working on the finish-report", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsStoredTerminalReportKind(tc.text); got != tc.want {
				t.Fatalf("terminal kind = %v, want %v", got, tc.want)
			}
		})
	}
}

func t761IdleBase() IdleNudgeObs {
	return IdleNudgeObs{
		Name:           t761Worker,
		Purpose:        claudia.PurposeWork,
		ProcessRunning: true,
		Phase:          "idle",
		IdleFor:        10 * time.Minute,
		HasOpenMission: true,
	}
}

func t761RecoverBase() FleetRecoverObs {
	return FleetRecoverObs{
		Name:           t761Worker,
		Purpose:        claudia.PurposeWork,
		ProcessRunning: true,
		HasOpenMission: true,
		PromptInFlight: true,
		SinceProgress:  DefaultFleetStuckTimeout + time.Second,
		NeedsRecover:   true,
		FailureClass:   agenterr.ClassUnknown,
	}
}

func TestT761ClassifyIdleNudgeSkipsStoredTerminalReport(t *testing.T) {
	t.Parallel()
	o := t761IdleBase()
	o.HasStoredTerminal = true
	act, reason := ClassifyIdleNudge(o)
	if act != IdleNudgeSkip || reason != "stored_terminal_report" {
		t.Fatalf("got %s/%s want skip/stored_terminal_report", act, reason)
	}
}

func TestT761ClassifyIdleNudgeSkipsTurnInFlight(t *testing.T) {
	t.Parallel()
	o := t761IdleBase()
	o.TurnInFlight = true
	act, reason := ClassifyIdleNudge(o)
	if act != IdleNudgeSkip || reason != IdleSkipInProgress {
		t.Fatalf("got %s/%s want skip/in_progress", act, reason)
	}
}

func TestT761ClassifyFleetRecoverSkipsStoredTerminalReport(t *testing.T) {
	t.Parallel()
	o := t761RecoverBase()
	o.HasStoredTerminal = true
	act, reason := ClassifyFleetRecover(o)
	if act != FleetRecoverSkip || reason != "stored_terminal_report" {
		t.Fatalf("got %s/%s want skip/stored_terminal_report", act, reason)
	}
}

func TestT761ClassifyFleetRecoverSkipsTurnInFlight(t *testing.T) {
	t.Parallel()
	o := t761RecoverBase()
	o.TurnInFlight = true
	act, reason := ClassifyFleetRecover(o)
	if act != FleetRecoverSkip || reason != "in_progress" {
		t.Fatalf("got %s/%s want skip/in_progress", act, reason)
	}
}

func TestT761StoredFinishReportSilencesSweep(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := agentreport.Save(dir, t761Worker, t761FinishReport, time.Now()); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: t761Worker, WorkDir: dir, SessionID: "s761",
		Purpose: claudia.PurposeWork, TargetID: "T761", AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	activity := NewIdleActivityTracker()
	now := time.Unix(9000, 0)
	activity.by[t761Worker] = IdleActivity{Phase: "idle", Updated: now.Add(-10 * time.Minute)}

	looks := NewReportStoreLooksSatisfied(dir)
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:                reg,
		Activity:           activity,
		Now:                now,
		LastTerminalReport: looks,
		ProcessRunning:     func(string) bool { return true },
	})
	for _, r := range reps {
		if r.Name != t761Worker {
			continue
		}
		if r.Action != IdleNudgeSkip || r.Reason != "stored_terminal_report" {
			t.Fatalf("classifier=%s/%s want skip/stored_terminal_report", r.Action, r.Reason)
		}
		return
	}
	t.Fatal("worker not evaluated")
}

func TestT761ConvergeStoredTerminalSatisfiesGap(t *testing.T) {
	t.Parallel()
	cond, _, reason := converge.ClassifyObservation(converge.Observation{
		Name:                 t761Worker,
		Purpose:              "work",
		Phase:                "idle",
		ProcessRunning:       true,
		MissionOpen:          true,
		StoredTerminalReport: true,
	})
	if cond != converge.ConditionSatisfied || reason != "stored_terminal_report" {
		t.Fatalf("got %s/%s want satisfied/stored_terminal_report", cond, reason)
	}
}

func TestT761AmbiguousWorkerIdleTextHasNoActLine(t *testing.T) {
	t.Parallel()
	text := FormatWorkerIdleAmbiguousText(WorkerIdleRef{Name: t761Worker, TargetID: "T761"},
		"stored report is not a typed finish-report or scout-report")
	if strings.Contains(text, "Act: continue") {
		t.Fatalf("ambiguous notice must not prescribe continue:\n%s", text)
	}
}

func TestT761WorkerIdleSuppressesOnStoredReport(t *testing.T) {
	dir := t.TempDir()
	s := &Server{stateDir: dir}
	s.SetAgentReportDir(dir)
	if _, err := agentreport.Save(dir, t761Worker, t761FinishReport, time.Now()); err != nil {
		t.Fatal(err)
	}
	suppress, reason := s.workerIdleSuppressReason(t761Worker)
	if !suppress || reason != "stored_terminal_report" {
		t.Fatalf("suppress=%v reason=%q want stored_terminal_report", suppress, reason)
	}
}
