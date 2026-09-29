// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T887: a sidecar seat (Claude on Oh My Pi) accepts a prompt and thinks for
// over a minute before its first text. The send's evidence window must settle
// on the acceptance, not report a working seat not_submitted — that is what
// happened to two relayed owner requests to jevons-po on 2026-09-28.
func TestT887AcceptedPromptSettlesTheSendBeforeAnyText(t *testing.T) {
	// A sidecar provider is a live-stream surface: events are the evidence.
	if providerKeepsClaudeTranscript(claudia.Provider("anthropic")) {
		t.Fatal("an anthropic sidecar seat was treated as a durable Claude transcript")
	}
	stub := &t501Stub{}
	const window = 200 * time.Millisecond
	watch := observeTurnFor(stub, "relay: status of T540?", window)
	go stub.publish(claudia.Event{Type: "progress", ProgressType: claudia.ProgressPromptAccepted})
	ev := watch()
	// The first text only arrives once the window has long closed.
	stub.publish(claudia.Event{Type: "assistant", Text: "T540 is waiting on the audit."})
	if !ev.Positive() || !ev.SessionEvent {
		t.Fatalf("an accepted prompt was not evidence: %+v", ev)
	}
	if err := ConfirmTurnBegan("sent", nil, ev); err != nil {
		t.Fatalf("the send was refused as not begun: %v", err)
	}
}

// A prompt the sidecar never starts still reports not submitted.
func TestT887ANeverStartedPromptIsStillNotSubmitted(t *testing.T) {
	stub := &t501Stub{}
	ev := observeTurnFor(stub, "relay: anything", 50*time.Millisecond)()
	if ev.Positive() {
		t.Fatalf("silence was taken as evidence: %+v", ev)
	}
	if !strings.Contains(ev.Detail, "no session event within") {
		t.Fatalf("detail = %q", ev.Detail)
	}
	if err := ConfirmTurnBegan("sent", nil, ev); err == nil {
		t.Fatal("a send that never began was confirmed")
	}
}
