// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

type recordingNotifier struct {
	mu   sync.Mutex
	sent []string
}

func (r *recordingNotifier) NotifyOwner(subject, kind, text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, subject+"|"+kind+"|"+text)
	return true
}

func (r *recordingNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// THE property of 🎯T415: the owner hears even when nothing else works.
//
// This Server has no registry, so the recovery agent cannot be created at
// all — which is the COMMON case exactly when it is most needed, since
// quota exhaustion and a daemon that cannot launch agents are precisely
// the faults being diagnosed. The notice must still go out.
//
// If someone later restructures this so the notice is emitted by the
// recovery agent, or only after a repair attempt fails, this test fails.
func TestOwnerIsNotifiedEvenWhenRecoveryCannotSpawn(t *testing.T) {
	n := &recordingNotifier{}
	s := New(t.TempDir(), nil, nil)
	s.SetOwnerNotifier(n)
	// No registry: spawnRecoveryAgent returns immediately.

	s.OnConvergenceExhausted(IdleNudgeReport{
		Name:   "jv-t999-stuck",
		Action: IdleNudgeMaxed,
		Reason: "max_nudges",
		Error:  "turn not submitted",
	})

	if n.count() != 1 {
		t.Fatalf("owner notices=%d want 1 — the alarm must not depend on the repair path", n.count())
	}
	got := n.sent[0]
	for _, want := range []string{"jv-t999-stuck", "convergence-exhausted", "given up", "max_nudges", "turn not submitted"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice missing %q:\n%s", want, got)
		}
	}
	// The owner must be told the agent was left alone on purpose,
	// otherwise "stuck" reads as "nobody looked".
	if !strings.Contains(got, "left stuck on purpose") {
		t.Errorf("notice does not explain the agent was deliberately not repaired:\n%s", got)
	}
}

// A stuck agent stays stuck. Repeating that every sweep would train the
// owner to ignore the surface, which is the failure mode an
// always-notify design has to respect.
func TestRepeatedExhaustionIsDeduplicated(t *testing.T) {
	n := &recordingNotifier{}
	s := New(t.TempDir(), nil, nil)
	s.SetOwnerNotifier(n)

	rep := IdleNudgeReport{Name: "jv-t999-stuck", Action: IdleNudgeMaxed, Reason: "max_nudges"}
	for i := 0; i < 5; i++ {
		s.OnConvergenceExhausted(rep)
	}
	if n.count() != 1 {
		t.Fatalf("notices=%d want 1 — the same agent failing the same way must not re-notify", n.count())
	}

	// A different agent is a different incident.
	s.OnConvergenceExhausted(IdleNudgeReport{Name: "jv-t998-other", Action: IdleNudgeMaxed, Reason: "max_nudges"})
	if n.count() != 2 {
		t.Fatalf("notices=%d want 2 — dedup must be per agent", n.count())
	}
}

// An unwired notifier is the pre-🎯T415 condition and must be loud rather
// than silently returning to the old behaviour.
func TestMissingNotifierDoesNotPanic(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.OnConvergenceExhausted(IdleNudgeReport{Name: "x", Action: IdleNudgeMaxed})
	// No assertion beyond not panicking: the ERROR log is the contract,
	// and the next test would catch a regression to silent success.
}

func TestExhaustionRenotifyWindow(t *testing.T) {
	var e exhaustionState
	now := time.Now()
	if !e.shouldNotify("a", now, time.Hour) {
		t.Fatal("first exhaustion must notify")
	}
	if e.shouldNotify("a", now.Add(59*time.Minute), time.Hour) {
		t.Error("re-notified inside the window")
	}
	if !e.shouldNotify("a", now.Add(61*time.Minute), time.Hour) {
		t.Error("did not re-notify after the window — a still-stuck agent is worth repeating eventually")
	}
}

// The other half of 🎯T415's oracle: when spawning IS possible, the
// recovery agent is actually created — and the stuck agent is not
// touched. Both were asserted in the target's acceptance and neither was
// covered by the first test, which only proved the notice survives when
// spawning is impossible.
func TestRecoveryAgentIsCreatedAndStuckAgentUntouched(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	stuck := "jv-t999-stuck"
	stuckSession := "11111111-2222-3333-4444-555555555555"
	if err := reg.Register(claudia.AgentDef{
		Name: stuck, WorkDir: t.TempDir(), SessionID: stuckSession,
		Provider: claudia.ProviderClaude, Materialized: true, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}

	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetOwnerNotifier(&recordingNotifier{})

	rep := IdleNudgeReport{Name: stuck, Action: IdleNudgeMaxed, Reason: "max_nudges"}
	// Called directly rather than via OnConvergenceExhausted, which
	// dispatches it in a goroutine. Launch will fail or start something
	// real; neither matters here — what matters is the registry row.
	defer reg.Stop(RecoveryAgentName(stuck))
	s.spawnRecoveryAgent(rep)

	rec := reg.Def(RecoveryAgentName(stuck))
	if rec == nil {
		t.Fatal("no recovery agent was registered — the diagnosis half never happens")
	}
	if rec.Purpose != claudia.PurposeAside {
		t.Errorf("recovery purpose=%q want aside — it is disposable, not a work agent", rec.Purpose)
	}
	if rec.Name == stuck {
		t.Fatal("recovery agent collided with the stuck agent")
	}

	// The stuck agent must be exactly as it was. Recovery destroys the
	// evidence, which is the whole reason it is left alone.
	after := reg.Def(stuck)
	if after == nil {
		t.Fatal("the stuck agent was removed")
	}
	if after.SessionID != stuckSession {
		t.Errorf("stuck agent session changed %s → %s; it was rotated, not diagnosed",
			stuckSession, after.SessionID)
	}
	if !after.Materialized {
		t.Error("stuck agent's Materialized was cleared — something tried to recover it")
	}
}

// The brief carries the instruction the whole design rests on. If this
// wording is lost, the recovery agent will helpfully repair the evidence.
func TestRecoveryBriefForbidsRepair(t *testing.T) {
	brief := FormatRecoveryBrief(IdleNudgeReport{
		Name: "jv-t999-stuck", Reason: "max_nudges", Error: "turn not submitted",
	})
	for _, want := range []string{
		"DO NOT restart, kill, or otherwise unstick",
		"repairing it first destroys the evidence",
		"File a bullseye target",
		"honest 'unknown' is worth more than a plausible guess",
		"jv-t999-stuck", "max_nudges", "turn not submitted",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

func TestRecoveryBriefNamesMissingDetail(t *testing.T) {
	brief := FormatRecoveryBrief(IdleNudgeReport{Name: "a"})
	if !strings.Contains(brief, "(none)") {
		t.Error("an absent reason/error should read as (none), not as an empty gap")
	}
}
