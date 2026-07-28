// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T46: the CEO prompt tells the overseer that worker replies arrive as
// notifications pushed into its conversation — this pins the mechanism
// behind that promise. If notify stops delivering (or stops naming the
// agent), the prompt's contract is broken and this fails.

func TestNotifyDeliversAgentReplyToOverseer(t *testing.T) {
	s := &Server{}

	var got string
	s.SetNotify(func(text string) { got = text })
	s.notify("maze-worker", "done: PR #7 is green")

	if !strings.Contains(got, "maze-worker") {
		t.Fatalf("notification does not name the agent: %q", got)
	}
	if !strings.Contains(got, "done: PR #7 is green") {
		t.Fatalf("notification lost the reply text: %q", got)
	}
}

// 🎯T61: a Grok worker signals turn-completion with a terminal *assistant*
// event (StopReason end_turn), never a "system" event. The event sink must
// deliver the accumulated reply to the overseer on that terminal stop —
// accumulating streaming chunks and text across mid-turn tool_use pauses,
// and NOT firing early. This is the exact seam where worker replies were
// silently lost after the Grok-only migration.
func TestAgentEventSinkNotifiesOnTerminalAssistantStop(t *testing.T) {
	s := &Server{}
	var got string
	s.SetNotify(func(text string) { got = text })
	sink := s.agentEventSink("jevons-po")

	// Streaming chunks accumulate; no notification mid-turn.
	sink(claudia.Event{Type: "assistant", Text: "Working"})
	sink(claudia.Event{Type: "assistant", Text: " on it"})
	if got != "" {
		t.Fatalf("notified mid-stream before turn completed: %q", got)
	}

	// A tool_use pause is mid-turn — still no notification.
	sink(claudia.Event{Type: "assistant", StopReason: "tool_use"})
	if got != "" {
		t.Fatalf("notified on mid-turn tool_use pause: %q", got)
	}

	// Terminal assistant stop delivers the whole turn's text, naming the agent.
	sink(claudia.Event{Type: "assistant", Text: " — done", StopReason: "end_turn"})
	if !strings.Contains(got, "jevons-po") {
		t.Fatalf("delivered reply does not name the agent: %q", got)
	}
	if !strings.Contains(got, "Working on it — done") {
		t.Fatalf("delivered reply lost accumulated text: %q", got)
	}
}

// A "system" event alone must NOT be treated as turn-complete for a Grok
// worker — the old trigger. With no terminal assistant stop, nothing is
// delivered (and nothing panics).
func TestAgentEventSinkIgnoresSystemEvent(t *testing.T) {
	s := &Server{}
	var calls int
	s.SetNotify(func(string) { calls++ })
	sink := s.agentEventSink("w")
	sink(claudia.Event{Type: "assistant", Text: "partial reply"})
	sink(claudia.Event{Type: "system"})
	if calls != 0 {
		t.Fatalf("a bare system event delivered a reply (%d calls); Grok never emits one as turn-complete", calls)
	}
}

// A worker reply with no notify sink must not panic — the daemon may not
// have attached the overseer yet.
func TestNotifyWithoutSinkIsSafe(t *testing.T) {
	s := &Server{}
	s.notify("orphan", "hello")
}

// Overlong replies are truncated, not dropped.
func TestNotifyTruncatesLongReplies(t *testing.T) {
	s := &Server{}
	var got string
	s.SetNotify(func(text string) { got = text })

	s.notify("w", strings.Repeat("x", 5000))
	if len(got) > 2100 {
		t.Fatalf("notification not truncated: len = %d", len(got))
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("truncated notification lacks ellipsis marker: %q", got)
	}
}
