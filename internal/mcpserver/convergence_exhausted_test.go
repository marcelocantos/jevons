// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"os"
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

// fakeRecoverBin writes an executable stand-in for bin/recover that
// records the argv it was called with, and returns its path. A real
// diagnostician would drive an agent for twenty minutes; what is under
// test is the dispatch, so the stand-in only has to be observable.
func fakeRecoverBin(t *testing.T, argvFile string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-recover")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argvFile + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// awaitArgv waits for the stand-in to record want arguments. The
// diagnostician is Start()ed and Release()d, never waited on — the
// product deliberately does not own it — so the test does the waiting the
// product refuses to do.
func awaitArgv(t *testing.T, argvFile string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if b, err := os.ReadFile(argvFile); err == nil {
			var argv []string
			for ln := range strings.SplitSeq(string(b), "\n") {
				if ln != "" {
					argv = append(argv, ln)
				}
			}
			if len(argv) >= want {
				return argv
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the diagnostician was never invoked: %s has fewer than %d arguments after 10s",
				argvFile, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ARM 1 — a diagnostician IS wired: the dispatch is an INVOCATION, and
// that is what this asserts.
//
// Deliberately no assertion about a registry row. 🎯T415 covered this half
// by asserting a row under RecoveryAgentName(stuck); 🎯T415.1 replaced the
// in-registry agent with a detached OS process, exactly so it outlives the
// daemon that may itself be the fault, and it registers nothing in either
// arm. The old assertion could not pass by any fixture and held master red
// for a day. Restoring a registry row to satisfy it would re-introduce the
// corpse accumulation 🎯T415.1 removed (🎯T420), so the row's absence is
// asserted the other way round: the registry ends with exactly the agent
// the test put in it.
//
// The flags are load-bearing, not decoration — cmd/recover exits 2 without
// -stuck, and -state/-repo are how a process that has outlived jevonsd
// finds the state it must diagnose without jevonsmcp.
func TestRecoveryDispatchesTheDetachedDiagnostician(t *testing.T) {
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

	repo := t.TempDir()
	stateDir := t.TempDir()
	argvFile := filepath.Join(t.TempDir(), "argv")

	s := New(repo, nil, nil)
	s.SetRegistry(reg)
	s.SetOwnerNotifier(&recordingNotifier{})
	s.SetRecoverBin(fakeRecoverBin(t, argvFile), stateDir)

	s.spawnRecoveryAgent(IdleNudgeReport{Name: stuck, Action: IdleNudgeMaxed, Reason: "max_nudges"})

	argv := awaitArgv(t, argvFile, 6)
	flags := map[string]string{}
	for i := 0; i+1 < len(argv); i += 2 {
		flags[argv[i]] = argv[i+1]
	}
	for flag, want := range map[string]string{
		"-stuck": stuck,
		"-state": stateDir,
		"-repo":  repo,
	} {
		if got := flags[flag]; got != want {
			t.Errorf("%s=%q want %q (argv=%q)", flag, got, want, argv)
		}
	}

	// The stuck agent must survive diagnosis untouched — recovery destroys
	// the evidence, which is the whole reason it is left alone.
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
	if n := len(reg.List()); n != 1 {
		t.Errorf("registry holds %d agents, want only the stuck one — the diagnostician is a detached"+
			" process and must not be parented to the daemon it may have to restart", n)
	}
}

// ARM 2 — no diagnostician is wired, which is EVERY hermetic construction:
// New(...) leaves recoverBin empty, so any test that does not call
// SetRecoverBin exercises this branch and proves nothing about arm 1.
// Production is wired at cmd/jevonsd/main.go:963.
//
// Asserting the WARN is the point. "Nothing was registered" is vacuously
// true here — nothing registers in either arm — so it cannot tell the
// early return apart from a dispatch that silently failed, and that
// indistinguishability is how the diagnosis half stayed a hermetic no-op
// unnoticed (🎯T420).
func TestRecoveryWithoutBinaryTakesTheWarnPath(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	s.spawnRecoveryAgent(IdleNudgeReport{Name: "jv-x", Action: IdleNudgeMaxed})

	var warned, dispatched bool
	for _, r := range cap.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "no recover binary wired") {
			warned = true
		}
		if strings.Contains(r.Message, "detached recovery dispatched") {
			dispatched = true
		}
	}
	if !warned {
		t.Error("no WARN that diagnosis was skipped — an unwired diagnostician must say so, not fail quietly")
	}
	if dispatched {
		t.Error("dispatch was reported with no binary wired")
	}
	if len(reg.List()) != 0 {
		t.Error("dispatch without a recover binary registered something")
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
