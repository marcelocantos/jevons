// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"strings"
	"testing"
)

func rec(kind Kind, extra func(*Record)) Record {
	r := Record{Kind: kind}
	if extra != nil {
		extra(&r)
	}
	return r
}

func TestT423EndedSessionIsIdle(t *testing.T) {
	// 76cef0a9 at 10:18:43Z: assistant text, then system close.
	recs := []Record{
		rec(KindUserMessage, func(r *Record) { r.Text = "please continue" }),
		rec(KindAssistant, func(r *Record) {
			r.Text = "done for now"
			r.StopReason = "end_turn"
		}),
		rec(KindOther, func(r *Record) { r.Type = "system" }),
	}
	if got := ClassifyPhase(recs); got != PhaseIdle {
		t.Fatalf("ended session classified %s, want idle", got)
	}
}

func TestT423MidTurnIsWorking(t *testing.T) {
	// jv-t374 mid-Playwright: tool_use, file growing, no end_turn.
	recs := []Record{
		rec(KindUserMessage, func(r *Record) { r.Text = "probe the cockpit" }),
		rec(KindAssistant, func(r *Record) { r.HasToolUse = true }),
	}
	if got := ClassifyPhase(recs); got != PhaseWorking {
		t.Fatalf("mid-turn classified %s, want working", got)
	}
}

func TestT423QueuedAttachmentIsWorking(t *testing.T) {
	recs := []Record{
		rec(KindQueueOp, func(r *Record) { r.Operation = "enqueue" }),
		rec(KindQueuedCommand, func(r *Record) { r.Queued = "brief" }),
	}
	if got := ClassifyPhase(recs); got != PhaseWorking {
		t.Fatalf("queued attachment classified %s, want working", got)
	}
}

func TestT423UnreadIsUnknownNotIdle(t *testing.T) {
	if got := ClassifyPhase(nil); got != PhaseUnknown {
		t.Fatalf("empty tape classified %s, want unknown", got)
	}
	if got := ClassifyPhaseFile("/no/such/session.jsonl"); got != PhaseUnknown {
		t.Fatalf("missing file classified %s, want unknown", got)
	}
}

func TestT423UnknownIsNotIdle(t *testing.T) {
	if PhaseUnknown.Positive() {
		t.Fatal("unknown must not be a positive reading")
	}
	if PhaseUnknown.String() == "idle" {
		t.Fatal("unknown rendered as idle")
	}
}

func TestT423DecodeAllThenClassify(t *testing.T) {
	tape := strings.NewReader(`{"type":"user","message":{"role":"user","content":"go"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}}
`)
	recs := DecodeAll(tape)
	if got := ClassifyPhase(recs); got != PhaseIdle {
		t.Fatalf("decoded tape classified %s, want idle; recs=%d", got, len(recs))
	}
}

func TestT423SystemicCap(t *testing.T) {
	if CapsSystemicActions(2, DefaultSystemicCap) {
		t.Fatal("two idle workers is ordinary, not systemic")
	}
	if !CapsSystemicActions(5, DefaultSystemicCap) {
		t.Fatal("five same-pass acts must trip the cap")
	}
}
