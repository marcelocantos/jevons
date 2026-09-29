// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func stallAt(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// Log lines in the daemon's own slog.TextHandler shape, copied from the
// isolate tails of gates 0b19696c and a80d7b58.
const (
	stallNoticeLine = `time=2026-09-22T08:01:51.000+10:00 level=INFO msg="notifying jevon" agent=bounce-aside-7b66 len=120 status=sent`
	stallNoticeSend = `time=2026-09-22T08:01:51.001+10:00 level=INFO msg=notify_queue component=notify_queue decision=drain depth=0 drained=1 owner_batch=false`
	stallOwnerQueue = `time=2026-09-22T08:02:05.100+10:00 level=INFO msg=agent_send component=agent_send name=jevons origin=owner status=delivered_unconfirmed queued=0 rehydrated=false outcome=queued_behind_turn payload_seen=false evidence="transcript x"`
	stallOwnerBegun = `time=2026-09-22T08:02:05.100+10:00 level=INFO msg=agent_send component=agent_send name=jevons origin=owner status=sent queued=0 rehydrated=false outcome=begun payload_seen=true evidence="transcript x"`
	stallAutoStart  = `time=2026-09-22T09:54:23.221+10:00 level=ERROR msg="auto-start failed" agent=jevons err="exclusive GROK_HOME unavailable for session 0154: stat /x/claudia/grok-homes/0154: no such file or directory"`
	stallNotRunning = `time=2026-09-22T09:54:23.221+10:00 level=ERROR msg="OVERSEER NOT RUNNING — chat cannot respond until this is fixed" overseer=jevons provider=grok`
	stallNoise      = `time=2026-09-22T08:02:10.000+10:00 level=INFO msg="🎯T418 handover sweep" pending=0`
)

func stallLogs(lines ...string) []byte { return []byte(strings.Join(lines, "\n") + "\n") }

func wantCauses(t *testing.T, got string, want, absent []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("diagnosis lacks %q:\n%s", w, got)
		}
	}
	for _, a := range absent {
		if strings.Contains(got, a) {
			t.Errorf("diagnosis wrongly names %q:\n%s", a, got)
		}
	}
}

// Gate 0b19696c (full grok run): J13 left the overseer on claude, the aside's
// reply notice held the seat with no answer, and the owner prompt queued
// behind it. All three named causes hold at once.
func TestT834SpecimenGrokRunNamesBusyBackendAndSilentProvider(t *testing.T) {
	notice := ownerStallPhase{At: stallAt(t, "2026-09-22T08:01:51.1+10:00"), Phase: "accepted", Correspondent: []string{"bounce-aside-7b66"}}
	thinking := ownerStallPhase{At: stallAt(t, "2026-09-22T08:01:52+10:00"), Phase: "thinking", Correspondent: []string{"bounce-aside-7b66"}}
	got := diagnoseOwnerStall(ownerStallEvidence{
		Requested: "grok", OverseerProvider: "claude",
		SentAt: stallAt(t, "2026-09-22T08:02:05+10:00"), AtSend: thinking,
		Phases: []ownerStallPhase{notice, thinking}, EchoIndex: 87,
		Logs: stallLogs(stallNoticeLine, stallNoticeSend, stallOwnerQueue, stallNoise),
	})
	wantCauses(t, got,
		[]string{"(a) overseer busy with a deferred notice", "thinking for bounce-aside-7b66", "outcome=queued_behind_turn",
			"last notice before send", "agent=bounce-aside-7b66",
			"(b) overseer on claude, not the requested grok",
			"(c) provider did not answer", "from 22:01:51Z"},
		[]string{"(d)", "(e)", "never echoed", "unclassified", "handover sweep"})
}

// Gate a80d7b58 (full claude run): J13 failed and left a Grok overseer that
// could not start. The diagnosis names the down overseer, not a silent
// provider.
func TestT834SpecimenClaudeRunNamesDownOverseer(t *testing.T) {
	down := ownerStallPhase{At: stallAt(t, "2026-09-22T09:54:24+10:00"), Phase: "idle", Down: "the overseer is not running"}
	got := diagnoseOwnerStall(ownerStallEvidence{
		Requested: "claude", OverseerProvider: "grok",
		SentAt: stallAt(t, "2026-09-22T09:54:30+10:00"), AtSend: down,
		Phases: []ownerStallPhase{down}, EchoIndex: 49,
		Logs: stallLogs(stallAutoStart, stallNotRunning),
	})
	wantCauses(t, got,
		[]string{"(b) overseer on grok, not the requested claude", "(d) overseer not running", "socket: the overseer is not running",
			"auto-start failed", "OVERSEER NOT RUNNING"},
		[]string{"(a)", "(c)", "(e)", "unclassified"})
}

// Control: an idle overseer on the requested backend took the owner prompt
// at once and never answered. Only the silent provider is named — a busy
// notice or backend switch that did not happen is not invented.
func TestT834ControlSilentProviderAlone(t *testing.T) {
	idle := ownerStallPhase{At: stallAt(t, "2026-09-22T08:02:00+10:00"), Phase: "idle"}
	owner := ownerStallPhase{At: stallAt(t, "2026-09-22T08:02:05.2+10:00"), Phase: "accepted"}
	got := diagnoseOwnerStall(ownerStallEvidence{
		Requested: "grok", OverseerProvider: "grok",
		SentAt: stallAt(t, "2026-09-22T08:02:05+10:00"), AtSend: idle,
		Phases: []ownerStallPhase{idle, owner}, EchoIndex: 12,
		// An earlier journey's owner prompt queued behind a turn is before
		// this send and must not be read as this one.
		Logs: stallLogs(strings.Replace(stallOwnerQueue, "08:02:05.100", "08:01:00.000", 1), stallOwnerBegun),
	})
	wantCauses(t, got, []string{"(c) provider did not answer", "stayed accepted from 22:02:05Z"},
		[]string{"(a)", "(b)", "(d)", "(e)", "unclassified"})
}

// An overseer that answered with other text is not a silent provider.
func TestT834AnsweredOtherTextIsNotSilence(t *testing.T) {
	idle := ownerStallPhase{Phase: "idle"}
	got := diagnoseOwnerStall(ownerStallEvidence{
		Requested: "grok", OverseerProvider: "grok", SentAt: time.Now(), AtSend: idle,
		Phases: []ownerStallPhase{idle}, EchoIndex: 5, Assistant: 1, Ended: 1, LastEnded: "I can't do that.",
	})
	wantCauses(t, got, []string{`(e) overseer answered 1 time(s) but not with the requested token; last: "I can't do that."`},
		[]string{"(a)", "(b)", "(c)", "(d)", "unclassified"})
}

func TestT834NoEvidenceSaysUnclassified(t *testing.T) {
	idle := ownerStallPhase{Phase: "idle"}
	got := diagnoseOwnerStall(ownerStallEvidence{Requested: "grok", OverseerProvider: "grok",
		SentAt: time.Now(), AtSend: idle, Phases: []ownerStallPhase{idle}, EchoIndex: 3})
	wantCauses(t, got, []string{"unclassified", "phases seen: [idle]"}, []string{"(a)", "(b)", "(c)", "(d)", "(e)"})
	got = diagnoseOwnerStall(ownerStallEvidence{Requested: "grok", SentAt: time.Now()})
	wantCauses(t, got, []string{"owner prompt never echoed"}, []string{"(b)", "unclassified"})
}

// The gather path reads the isolate's own registry and the daemon log from
// the journey's start offset, and names a read it could not make.
func TestT834DiagnosisReadsRegistryAndLogSlice(t *testing.T) {
	dir := t.TempDir()
	registry, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(claudia.AgentDef{Name: overseerName, SessionID: "s", Provider: claudia.ProviderClaude, WorkDir: dir}); err != nil {
		t.Fatal(err)
	}
	earlier := stallLogs(stallNotRunning) // before J14 began: not this journey's
	logPath := filepath.Join(dir, "jevonsd.log")
	if err := os.WriteFile(logPath, append(earlier, stallLogs(stallOwnerQueue)...), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &suite{stateDir: dir, logPath: logPath, provider: claudia.ProviderGrok}
	got := s.ownerStallDiagnosis(&ownerStallWatch{}, ownerStallPhase{Phase: "idle"},
		stallAt(t, "2026-09-22T08:02:00+10:00"), int64(len(earlier)),
		errors.New("owner mux reply (owner index=87): context deadline exceeded"))
	wantCauses(t, got, []string{"(b) overseer on claude, not the requested grok", "(a) overseer busy", "queued_behind_turn"},
		[]string{"(d)", "never echoed", "unread"})

	s.logPath = filepath.Join(dir, "missing.log")
	got = s.ownerStallDiagnosis(&ownerStallWatch{}, ownerStallPhase{}, time.Now(), 0, errors.New("owner mux reply (owner index=0): context deadline exceeded"))
	wantCauses(t, got, []string{"daemon log unread", "never echoed"}, nil)
}

// The tap forwards every frame unchanged and in order while recording the
// phase transitions it saw, window meta included.
func TestT834TapForwardsAndRecordsPhases(t *testing.T) {
	meta := func(body string) []byte {
		data, _ := json.Marshal(map[string]any{"v": 1, "ch": ownerMuxChannel, "t": "meta", "body": json.RawMessage(body)})
		return data
	}
	in := make(chan []byte, 8)
	sent := [][]byte{
		meta(`{"n":3,"lo":1,"hi":0,"following":true,"phase":{"phase":"thinking","correspondent":["aside-1"]},"overseer_down":""}`),
		[]byte(`{"v":1,"ch":"other","t":"meta","body":{"phase":{"phase":"idle"}}}`),
		meta(`{"phase":{"phase":"thinking","correspondent":["aside-1"]}}`), // same sample: not a transition
		meta(`{"working":true}`), // no phase: ignored
		meta(`{"phase":{"phase":"idle"},"overseer_down":"the overseer is not running"}`),
	}
	for _, d := range sent {
		in <- d
	}
	close(in)
	w := &ownerStallWatch{}
	var got [][]byte
	for d := range w.tap(context.Background(), in) {
		got = append(got, d)
	}
	if len(got) != len(sent) {
		t.Fatalf("forwarded %d frames, want %d", len(got), len(sent))
	}
	for i := range sent {
		if string(got[i]) != string(sent[i]) {
			t.Fatalf("frame %d changed in the tap", i)
		}
	}
	phases, _, _, _ := w.snapshot()
	if len(phases) != 2 || phases[0].String() != "thinking for aside-1" ||
		phases[1].String() != "idle (down: the overseer is not running)" {
		t.Fatalf("phases = %v", phases)
	}
}

func TestT834OwnerEchoIndex(t *testing.T) {
	if got := ownerEchoIndex(errors.New("owner mux reply (owner index=87): context deadline exceeded")); got != 87 {
		t.Fatalf("index = %d", got)
	}
	if got := ownerEchoIndex(errors.New("mux closed")); got != 0 {
		t.Fatalf("index = %d", got)
	}
}
