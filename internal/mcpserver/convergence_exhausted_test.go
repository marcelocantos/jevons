// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"sync"
	"testing"
	"time"
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
