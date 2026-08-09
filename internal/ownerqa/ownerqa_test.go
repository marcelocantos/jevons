// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownerqa

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- fixture builders: the exact journal shapes internal/server writes ---

func ownerTurn(text string) string {
	b, _ := json.Marshal(map[string]any{
		"type":        "user",
		"turn_origin": "owner",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	})
	return string(b)
}

// agentNote is a worker report injected on the user role (🎯T381). It is not
// the owner speaking and must never open a question window.
func agentNote(text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "user", "turn_origin": "agent",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	})
	return string(b)
}

func visibleReply(text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":        "assistant",
			"content":     []map[string]any{{"type": "text", "text": text}},
			"stop_reason": "end_turn",
		},
	})
	return string(b)
}

// noOpTurn is the wire shape a sealed turn takes when it painted nothing: the
// empty end_turn chat_wire.go emits for a [silent] stream, and the same shape
// a turn that produced no text at all seals with. This is the 019fe5e8 record.
func noOpTurn() string {
	b, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":        "assistant",
			"content":     []any{},
			"stop_reason": "end_turn",
		},
	})
	return string(b)
}

func errorFrame(text string) string {
	b, _ := json.Marshal(map[string]any{"type": "error", "error": text})
	return string(b)
}

func statusFrame() string {
	b, _ := json.Marshal(map[string]any{"type": "status", "state": "idle"})
	return string(b)
}

// TestIsQuestionHoldsTheSilentDistinction pins the predicate that the whole
// detector rests on. Over-broadness here is the failure mode that gets a
// detector switched off, so the negative half of this table matters more than
// the positive half.
func TestIsQuestionHoldsTheSilentDistinction(t *testing.T) {
	questions := []string{
		// The actual 019fe5e8 question.
		"Orthograph is giving Pippa a 127.0.0.1 address... haven't we integrated pigeon?",
		"why is the UI not rendering markdown?",
		"What happened to the relay",
		"Is the daemon up?",
		"can you check whether the port is bound?",
		"how do I re-pair the device",
		"Two things. First the build is red.\nShould I revert it?",
		"[user]\nwhat is the status of T378?",
		"**why did that fail?**",
	}
	for _, q := range questions {
		if !IsQuestion(q) {
			t.Errorf("IsQuestion(%q) = false, want true — an owner question must never read as routine", q)
		}
	}

	// Routine status/resume/directive traffic. Every one of these is legitimately
	// answerable with [silent]; if any reads as a question the detector fires on
	// healthy supervision traffic and is worthless.
	notQuestions := []string{
		"continue",
		"ok",
		"go",
		"ship it",
		"fix the build",
		"restart the daemon",
		"target: an owner question never goes unanswered",
		"push as you go",
		"merge to master locally",
		"Should be fine.",
		"Can't reproduce it, moving on.",
		"status update: T378 in progress",
		"proceed with the plan",
		"Do the T385 work first.",
		"whatever you think is best",
		"", "   ", "\n",
	}
	for _, s := range notQuestions {
		if IsQuestion(s) {
			t.Errorf("IsQuestion(%q) = true, want false — routine traffic must stay out of scope", s)
		}
	}
}

// TestScanFlagsTheIncidentShape is the 019fe5e8 reconstruction: the owner asks,
// the overseer escalates and intends a relay, and then burns turn after turn
// producing nothing owner-visible. The question is still open at handover.
func TestScanFlagsTheIncidentShape(t *testing.T) {
	lines := []string{
		ownerTurn("Orthograph is giving Pippa a 127.0.0.1 address... haven't we integrated pigeon?"),
		// The escalation and the answer coming back — none of it owner-visible.
		agentNote("[Agent orthograph-po responded] pigeon relay is integrated; the device needs re-pairing"),
	}
	// "Relay to owner briefly with re-pair steps" — and then ~100 no-ops.
	for range 100 {
		lines = append(lines, noOpTurn())
	}

	found := Scan(lines, 0)
	if len(found) != 1 {
		t.Fatalf("Scan found %d findings, want exactly 1 — the owner's question went unanswered", len(found))
	}
	f := found[0]
	if f.Kind != KindNoOpLoop {
		t.Errorf("Kind = %q, want %q — 100 consecutive no-op turns with a question open is a degenerate loop, not silence", f.Kind, KindNoOpLoop)
	}
	if f.NoOpTurns != 100 {
		t.Errorf("NoOpTurns = %d, want 100", f.NoOpTurns)
	}
	if !strings.Contains(f.Question, "127.0.0.1") {
		t.Errorf("Question = %q, want the owner's actual words", f.Question)
	}
	if f.Line != 0 {
		t.Errorf("Line = %d, want 0", f.Line)
	}
}

// TestScanIsQuietOnHealthySessions is the over-broadness guard. Every case
// here is a session that works, including heavy legitimate [silent] traffic.
// A detector that reports anything on these fires all day and gets deleted.
func TestScanIsQuietOnHealthySessions(t *testing.T) {
	cases := map[string][]string{
		"question answered directly": {
			ownerTurn("haven't we integrated pigeon?"),
			visibleReply("We have — Pippa just needs re-pairing. Steps: …"),
		},
		"routine status turn answered silent": {
			ownerTurn("continue"),
			noOpTurn(),
		},
		// The hard distinction, stated as a test: a long run of silent
		// supervision turns with no owner question outstanding is healthy.
		"long silent supervision run with no question": {
			ownerTurn("ship it"),
			noOpTurn(), noOpTurn(), noOpTurn(), noOpTurn(), noOpTurn(),
			noOpTurn(), noOpTurn(), noOpTurn(), noOpTurn(), noOpTurn(),
		},
		"question answered after some quiet work": {
			ownerTurn("what is the status of T378?"),
			noOpTurn(), noOpTurn(),
			visibleReply("In progress — oracle landed, wiring next."),
		},
		"delivery failure is a visible outcome, not silence": {
			ownerTurn("is the daemon up?"),
			errorFrame("message not delivered: provider unavailable"),
		},
		"agent notes never open a question window": {
			agentNote("[Agent jv-t378 responded] should I also cover T388?"),
			noOpTurn(), noOpTurn(), noOpTurn(), noOpTurn(),
		},
		"status frames are not turns": {
			ownerTurn("go"),
			statusFrame(), statusFrame(), statusFrame(), statusFrame(),
		},
		"directive with no answer owed": {
			ownerTurn("fix the build"),
			noOpTurn(), noOpTurn(), noOpTurn(), noOpTurn(),
		},
		"empty log": {},
	}
	for name, lines := range cases {
		if found := Scan(lines, 0); len(found) != 0 {
			t.Errorf("%s: Scan reported %d findings, want 0 — healthy traffic must stay quiet (got %+v)", name, len(found), found)
		}
	}
}

// TestScanReportsPlainUnansweredQuestion covers the milder half of acceptance
// 1: a question that simply never got an answer, below the no-op loop bar.
func TestScanReportsPlainUnansweredQuestion(t *testing.T) {
	found := Scan([]string{
		ownerTurn("why did the relay drop?"),
		noOpTurn(),
		// The owner gives up and moves on — the question is still unanswered.
		ownerTurn("never mind, restart it"),
		visibleReply("Restarted."),
	}, 0)
	if len(found) != 1 {
		t.Fatalf("Scan found %d findings, want 1", len(found))
	}
	if found[0].Kind != KindUnanswered {
		t.Errorf("Kind = %q, want %q — one no-op turn is not yet a loop", found[0].Kind, KindUnanswered)
	}
	if found[0].NoOpTurns != 1 {
		t.Errorf("NoOpTurns = %d, want 1", found[0].NoOpTurns)
	}
}

// TestScanToleratesLegacyAndMalformedJournals: journals on disk predate the
// 🎯T384 typed-content shape, and a torn line must never panic a scan.
func TestScanToleratesLegacyAndMalformedJournals(t *testing.T) {
	legacyOwner := `{"type":"user","message":{"role":"user","content":"haven't we integrated pigeon?"}}`
	legacyReply := `{"type":"assistant","message":{"role":"assistant","content":"we have"}}`

	if found := Scan([]string{legacyOwner, legacyReply}, 0); len(found) != 0 {
		t.Errorf("legacy bare-string journal: got %d findings, want 0", len(found))
	}
	// Legacy owner question, no answer: still detected.
	if found := Scan([]string{legacyOwner, noOpTurn()}, 0); len(found) != 1 {
		t.Errorf("legacy unanswered question: got %d findings, want 1", len(found))
	}
	// A torn final line (crash mid-append) is skipped, not fatal.
	if found := Scan([]string{ownerTurn("why?"), `{"type":"assis`}, 0); len(found) != 1 {
		t.Errorf("torn journal: got %d findings, want 1", len(found))
	}
}
