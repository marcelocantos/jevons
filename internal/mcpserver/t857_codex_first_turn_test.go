// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T857 — a freshly minted codex seat must actually begin its first turn,
// or the mint must fail loudly. It did not: claudia's codex app-server
// backend publishes a bare Type="system" thread/start ack the instant the
// thread object is created, before codex has accepted — let alone begun —
// the submitted brief. Before this fix, observeTurnForCancelable's
// live-stream branch treated ANY published event as turn evidence, so that
// single ack alone confirmed the turn and the daemon logged
// prompt_delivered=true. Two live probes (jv-t849-codex-probe,
// jv-t849-git-write-probe, 2026-09-22) reproduced exactly this: one ack,
// then nothing — no rollout file, no transcript, no tool call, ever.
//
// codexSubstantiveEvent(claudia.Event) is the pure predicate that tells a
// thread/start ack (Type="system", no TurnID, not an error) apart from real
// turn evidence (anything with a TurnID, or an error). It is wired into the
// codex live-stream watch via observeTurnForCancelableFiltered.

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// The pure predicate: a bare thread/start ack is not turn evidence, but a
// TurnID-bearing event and an error event both are.
func TestCodexSubstantiveEventRejectsThreadStartAck(t *testing.T) {
	t.Parallel()

	ack := claudia.Event{Type: "system", Text: "thread-abc123"} // thread/start shape
	if codexSubstantiveEvent(ack) {
		t.Fatal("a bare thread/start ack (system, no TurnID, not an error) must not count as turn evidence")
	}

	assistant := claudia.Event{Type: "assistant", TurnID: "turn-1", Text: "hi"}
	if !codexSubstantiveEvent(assistant) {
		t.Fatal("an event carrying a TurnID is real turn evidence")
	}

	progress := claudia.Event{Type: "progress", TurnID: "turn-1"}
	if !codexSubstantiveEvent(progress) {
		t.Fatal("a tool-use progress event with a TurnID is real turn evidence")
	}

	errEv := claudia.Event{Type: "system", IsError: true, Text: "boom"}
	if !codexSubstantiveEvent(errEv) {
		t.Fatal("an error event is real evidence the turn was attempted, even without a TurnID")
	}
}

// End to end: a codex-shaped live-stream watch that sees only the
// thread/start ack must time out unbegun (the specimen), not confirm.
func TestCodexWatchDoesNotConfirmOnThreadStartAckAlone(t *testing.T) {
	t.Parallel()

	stub := &t501Stub{path: "/nonexistent/never-written.jsonl"}
	watch, cancel := observeTurnForCancelableFiltered(
		liveStreamObserver{stub}, "", shortWindow, codexSubstantiveEvent)
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		stub.publish(claudia.Event{Type: "system", Text: "thread-abc123"})
	}()

	ev := watch()
	if ev.Positive() {
		t.Fatalf("a lone thread/start ack must not confirm a begun turn: %+v", ev)
	}
}

// The same watch DOES confirm once codex publishes real turn-shaped
// activity (a TurnID-bearing event), same as before this fix for every
// event that was never the false-positive ack.
func TestCodexWatchConfirmsOnRealTurnEvent(t *testing.T) {
	t.Parallel()

	stub := &t501Stub{path: "/nonexistent/never-written.jsonl"}
	watch, cancel := observeTurnForCancelableFiltered(
		liveStreamObserver{stub}, "", 5*time.Second, codexSubstantiveEvent)
	defer cancel()

	go func() {
		time.Sleep(10 * time.Millisecond)
		stub.publish(claudia.Event{Type: "system", Text: "thread-abc123"}) // ack, ignored
		time.Sleep(10 * time.Millisecond)
		stub.publish(claudia.Event{Type: "assistant", TurnID: "turn-1", Text: "working"})
	}()

	ev := watch()
	if !ev.Positive() || !ev.SessionEvent {
		t.Fatalf("a real turn-shaped event must confirm: %+v", ev)
	}
}
