// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"

	"github.com/marcelocantos/claudia"
)

// t919Server is a bare server with a /ws/mux session subscribed to the
// overseer transcript, so the test reads the same level the cockpit (and
// J3's waitBootSweepQuiet) reads.
func t919Server(t *testing.T) (*Server, *muxSession) {
	t.Helper()
	s := &Server{}
	s.overseerName = "jevons"
	s.notifySender = func(string) error { return nil }
	h := newMuxHub()
	sess := &muxSession{send: make(chan []byte, 256), transcripts: map[string]*muxWatch{"jevons": {subscribed: true}}}
	h.add(sess)
	s.mux = h
	return s, sess
}

// lastMuxPhase drains the session and returns the phase of the last meta
// frame that carried one ("" when none did).
func lastMuxPhase(t *testing.T, sess *muxSession) string {
	t.Helper()
	last := ""
	for {
		select {
		case raw := <-sess.send:
			var env muxEnvelope
			if err := json.Unmarshal(raw, &env); err != nil {
				t.Fatal(err)
			}
			if env.T != "meta" {
				continue
			}
			var body struct {
				Phase *PhaseSample `json:"phase"`
			}
			if err := json.Unmarshal(env.Body, &body); err != nil {
				t.Fatal(err)
			}
			if body.Phase != nil {
				last = body.Phase.Phase
			}
		default:
			return last
		}
	}
}

func assertOverseerIdle(t *testing.T, s *Server, sess *muxSession) {
	t.Helper()
	if p := s.OverseerPhase(); p.Phase != PhaseIdle {
		t.Fatalf("history_meta phase after the turn ended = %+v, want idle", p)
	}
	if got := lastMuxPhase(t, sess); got != PhaseIdle {
		t.Fatalf("last /ws/mux overseer phase = %q, want idle", got)
	}
}

// 🎯T919: the observed J3 failure (gate 8a6208f7, provider=claude). The
// post-boot resume turn's JSONL is a thinking-only record and then the
// "[silent] …" text record, both stop_reason=end_turn. The Claude TUI pane
// poller runs on its own clock and published a tui_preview for the ⏺ block
// between them. The preview mapped to phase=tool, and the silent terminal
// stop returned before the phase reduce, so the mux level kept saying
// tool until shutdown.
func TestT919SilentResumeTurnWithPanePreviewEndsIdle(t *testing.T) {
	s, sess := t919Server(t)
	_ = s.SendToOverseer("[daemon-restarted] The development daemon restarted.")
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"}) // thinking-only record
	s.DeliverOverseerEvent(claudia.Event{
		Type:         "progress",
		ProgressType: claudia.ProgressTUIPreview,
		Text:         "[silent] The development daemon has reattached.",
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:       "assistant",
		Text:       "[silent] The development daemon has reattached. No work children.",
		StopReason: "end_turn",
	})
	assertOverseerIdle(t, s, sess)
}

// 🎯T919: a silent terminal stop is a terminal stop. The silent path used to
// return before the phase reduce, so whatever the turn last showed (here a
// tool) outlived the turn.
func TestT919SilentTerminalStopClearsToolPhase(t *testing.T) {
	s, sess := t919Server(t)
	_ = s.SendToOverseer("[Agent jevons-po responded]\nreport")
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressToolUse, ToolTitle: "Read", ToolStatus: "in_progress"})
	if p := s.OverseerPhase(); p.Phase != PhaseTool {
		t.Fatalf("mid-turn tool phase = %+v", p)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "[silent] noted", StopReason: "end_turn"})
	assertOverseerIdle(t, s, sess)

	// Grok shape: silent deltas, then a body-less terminal assistant event.
	_ = s.SendToOverseer("[Agent jevons-po responded]\nanother")
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressToolUse, ToolTitle: "Read", ToolStatus: "in_progress"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "[silent] "})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "noted"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	assertOverseerIdle(t, s, sess)
}

// 🎯T919: a progress event that lands behind the terminal stop — a pane
// preview, a tool_call_update from the finished turn — does not reopen a
// busy phase. The next turn's opener still does.
func TestT919LateProgressAfterTerminalStopStaysIdle(t *testing.T) {
	s, sess := t919Server(t)
	_ = s.SendToOverseer(userTurnPrefix + "hello")
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "hi there"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressToolUse, ToolTitle: "Read", ToolStatus: "in_progress"})
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressTUIPreview, Text: "hi there"})
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressThought})
	assertOverseerIdle(t, s, sess)

	// A new turn opens on the provider's own signal, with no drain.
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressPromptAccepted})
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressToolUse, ToolTitle: "Read", ToolStatus: "in_progress"})
	if p := s.OverseerPhase(); p.Phase != PhaseTool || p.Step != "Read" {
		t.Fatalf("opened turn did not reach tool: %+v", p)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "done", StopReason: "end_turn"})
	assertOverseerIdle(t, s, sess)

	// Claude opens a turn with the prompt echo.
	s.DeliverOverseerEvent(claudia.Event{Type: "user", Text: "next"})
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressThought})
	if p := s.OverseerPhase(); p.Phase != PhaseThinking {
		t.Fatalf("echo-opened turn did not reach thinking: %+v", p)
	}
}

// 🎯T919: a Claude TUI pane preview is provisional assistant text, not a
// tool; a preview fault and a superseded prompt are not a phase at all.
func TestT919PreviewIsStreamingNotTool(t *testing.T) {
	p, ok := phaseFromEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressTUIPreview, Text: "hel"})
	if !ok || p.Phase != PhaseStreaming {
		t.Fatalf("tui_preview = %+v ok=%v, want streaming", p, ok)
	}
	for _, pt := range []string{claudia.ProgressTUIPreviewFault, claudia.ProgressPromptSuperseded} {
		if p, ok := phaseFromEvent(claudia.Event{Type: "progress", ProgressType: pt}); ok {
			t.Fatalf("%s painted phase %+v", pt, p)
		}
	}
}

// 🎯T919: the terminal stop's settle drains the next queued batch, which
// stamps it accepted. The finished turn's idle must land before that stamp,
// not after it — otherwise the new turn reads idle, and with the rest guard
// its own thoughts and tools could not move it off idle.
func TestT919DrainOnTerminalStopLeavesNextTurnAccepted(t *testing.T) {
	s, sess := t919Server(t)
	_ = s.SendToOverseer(userTurnPrefix + "owner speaks")
	_ = s.SendToOverseer("[Agent jevons-po responded]\nqueued behind owner")
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "answer", StopReason: "end_turn"})
	p := s.OverseerPhase()
	if p.Phase != PhaseAccepted || len(p.Correspondent) != 1 || p.Correspondent[0] != "jevons-po" {
		t.Fatalf("drained batch after the owner's seal = %+v, want accepted for jevons-po", p)
	}
	if got := lastMuxPhase(t, sess); got != PhaseAccepted {
		t.Fatalf("last /ws/mux overseer phase = %q, want accepted", got)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "progress", ProgressType: claudia.ProgressThought})
	if p := s.OverseerPhase(); p.Phase != PhaseThinking {
		t.Fatalf("drained turn's thought = %+v, want thinking", p)
	}
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "[silent] ok", StopReason: "end_turn"})
	assertOverseerIdle(t, s, sess)
}
