// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T752 — the acceptance hermetic. An adopted Grok Build seat whose
// chat_history.jsonl holds a whole GOAL_STATUS: complete line gets its turn
// stored as a report AND its Session Goal cleared, so "Continue the open
// objective" stops. The control arm is what makes that sharp: the same file
// shape with an ordinary turn must leave the Goal intact, so a close here
// cannot be the recovery closing indiscriminately.
//
// Specimen: jv-t742-rule-scan-scope, session 01a0bff0, 2026-09-21 — multiple
// finish-reports and GOAL_STATUS lines in the transcript, an empty
// jevons_agent_report_read, and Continue firing forever.

const t752Goal = "Achieve 🎯T752. Work until evidenced complete or blocked."

// t752GrokAssistant writes a Grok Build assistant record. Grok puts the text
// in a top-level content string and marks a mid-turn record with tool_calls;
// there is no stop_reason and no timestamp anywhere in the shape, which is
// exactly why the Claude-shaped reader saw nothing here.
func t752GrokAssistant(t *testing.T, text string, toolCalls bool) string {
	t.Helper()
	rec := map[string]any{
		"type":     "assistant",
		"content":  text,
		"model_id": "grok-4.6",
	}
	if toolCalls {
		rec["tool_calls"] = []any{map[string]any{"id": "call-t752-0"}}
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func t752GrokUser(t *testing.T, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":    "user",
		"content": []any{map[string]any{"type": "text", "text": text}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// t752GrokHistory writes a Grok chat_history.jsonl. The basename is what picks
// the parser, so it must be the real one.
func t752GrokHistory(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), GrokChatHistoryFile)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// t752Seat is a work seat with an OPEN Session Goal, which is the state the
// specimen was stuck in.
func t752Seat(t *testing.T) (*Server, *fakeSender, string) {
	t.Helper()
	s, po, _, reportDir := t690Server(t)
	def := s.registry.Def(t690Worker)
	if def == nil {
		t.Fatalf("fixture seat %s is not registered", t690Worker)
	}
	withGoal := *def
	withGoal.Purpose = claudia.PurposeWork
	withGoal.Goal = t752Goal
	withGoal.TargetID = "T752"
	withGoal.SessionID = "01a0bff0-0000-0000-0000-000000000000"
	if err := s.registry.Register(withGoal); err != nil {
		t.Fatalf("register seat with an open Goal: %v", err)
	}
	return s, po, reportDir
}

func t752FinishReport(marker bool) string {
	body := "```jevons\njevons: kind finish-report\njevons: target T752\njevons: silent-ledger none\n```\n" +
		"The Grok arm reads chat_history and the recovery closes the mission."
	if marker {
		return body + "\n\n" + claudia.GoalStatusComplete
	}
	return body + "\n\nStill wiring the parser; no verdict yet."
}

// TestT752GrokGoalStatusStoresReportAndClearsGoal is the row's acceptance.
func TestT752GrokGoalStatusStoresReportAndClearsGoal(t *testing.T) {
	s, po, reportDir := t752Seat(t)
	report := t752FinishReport(true)

	path := t752GrokHistory(t,
		t752GrokUser(t, t752Goal),
		t752GrokAssistant(t, "Reading the transcript shape first.", true),
		t752GrokAssistant(t, report, false),
	)

	if n := s.recoverGrokMissedTurns(t690Worker, path); n != 1 {
		t.Fatalf("recovered turns = %d, want 1", n)
	}

	// Clause one: the turn is stored as a report, so jevons_agent_report_read
	// is no longer empty for this seat.
	rec, err := agentreport.Latest(reportDir, t690Worker)
	if err != nil {
		t.Fatalf("stored report: %v", err)
	}
	if !strings.Contains(rec.Text, "finish-report") ||
		!strings.Contains(rec.Text, claudia.GoalStatusComplete) {
		t.Fatalf("stored report is not the turn the seat wrote: %q", rec.Text)
	}

	// Clause two: the Goal is cleared, so Continue is not injected again.
	if def := s.registry.Def(t690Worker); def != nil && strings.TrimSpace(def.Goal) != "" {
		t.Fatalf("GOAL_STATUS: complete in chat_history must clear the Goal; Goal=%q", def.Goal)
	}

	// The parent heard it, which is the 🎯T690 channel the recovery rides.
	var all string
	for _, m := range po.sent {
		all += m + "\n"
	}
	if !strings.Contains(all, "finish-report") && s.pendingAgentSends(t690Parent) == 0 {
		t.Fatalf("parent neither received nor queued the recovered report: %v", po.sent)
	}

	// Idempotent: a re-attach finds the same body already stored (Grok records
	// carry no timestamp, so this dedupe is the only thing standing between a
	// sweep and a replay storm).
	if n := s.recoverGrokMissedTurns(t690Worker, path); n != 0 {
		t.Fatalf("second recovery = %d, want 0 — an identical body is not a new turn", n)
	}
}

// TestT752OrdinaryGrokTurnDoesNotClose is the control arm.
func TestT752OrdinaryGrokTurnDoesNotClose(t *testing.T) {
	s, _, reportDir := t752Seat(t)

	path := t752GrokHistory(t,
		t752GrokUser(t, t752Goal),
		t752GrokAssistant(t, "Reading the transcript shape first.", true),
		t752GrokAssistant(t, t752FinishReport(false), false),
	)

	if n := s.recoverGrokMissedTurns(t690Worker, path); n != 1 {
		t.Fatalf("recovered turns = %d, want 1 (the turn is still a turn)", n)
	}
	if rec, err := agentreport.Latest(reportDir, t690Worker); err != nil ||
		!strings.Contains(rec.Text, "no verdict yet") {
		t.Fatalf("an ordinary turn is still stored: %q, %v", rec.Text, err)
	}
	def := s.registry.Def(t690Worker)
	if def == nil {
		t.Fatal("control: an ordinary turn must not remove the seat")
	}
	if strings.TrimSpace(def.Goal) == "" {
		t.Fatal("control: an ordinary turn without the marker must NOT close the Goal")
	}
}

// TestT752GrokTurnsAreSegmentedByToolCalls guards the parse itself. Without
// the tool_calls boundary every assistant record in the file concatenates into
// one turn, and the stored "report" becomes the whole session.
func TestT752GrokTurnsAreSegmentedByToolCalls(t *testing.T) {
	t.Parallel()
	path := t752GrokHistory(t,
		t752GrokUser(t, "first prompt"),
		t752GrokAssistant(t, "turn one prose. ", true),
		t752GrokAssistant(t, "turn one done.", false),
		t752GrokUser(t, "second prompt"),
		t752GrokAssistant(t, "turn two done.", false),
	)
	turns, err := scanGrokMissedTurns(path)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("turns = %d (%v), want 2", len(turns), turns)
	}
	if turns[0].Text != "turn one prose. turn one done." {
		t.Fatalf("turn one = %q; mid-turn prose must accumulate into its own turn", turns[0].Text)
	}
	if turns[1].Text != "turn two done." {
		t.Fatalf("turn two = %q; a prompt must reset the accumulator", turns[1].Text)
	}
}

// TestT752DanglingToolCallDoesNotBleedIntoNextTurn covers the interrupted
// turn: it never flushes, and its orphaned prose must not be prepended to the
// next turn's report.
func TestT752DanglingToolCallDoesNotBleedIntoNextTurn(t *testing.T) {
	t.Parallel()
	path := t752GrokHistory(t,
		t752GrokAssistant(t, "interrupted mid-tool-call", true),
		t752GrokUser(t, "new prompt after the interrupt"),
		t752GrokAssistant(t, "clean answer.", false),
	)
	turns, err := scanGrokMissedTurns(path)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns = %v, %v; want exactly the clean answer", turns, err)
	}
	if turns[0].Text != "clean answer." {
		t.Fatalf("turn = %q; orphaned prose bled across the prompt", turns[0].Text)
	}
}

// TestT752SeatTranscriptPathFallsBackToSessionStore covers the other half of
// why the specimen was silent: a Grok ACP seat's process reports no JSONL path
// at all, so the recovery used to skip it on that emptiness alone.
func TestT752SeatTranscriptPathFallsBackToSessionStore(t *testing.T) {
	s, _, _ := t752Seat(t)
	sessionID := s.registry.Def(t690Worker).SessionID

	root := t.TempDir()
	bucket := filepath.Join(root, "%2FUsers%2Fmarcelo%2Fwork%2Fjevons", sessionID)
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(bucket, GrokChatHistoryFile)
	if err := os.WriteFile(want, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := s.seatTranscriptPath(t690Worker, nil); got != "" {
		t.Fatalf("unwired store must resolve nothing, got %q", got)
	}
	s.SetGrokSessionsDir(root)
	if got := s.seatTranscriptPath(t690Worker, nil); got != want {
		t.Fatalf("seatTranscriptPath = %q, want %q", got, want)
	}
	if !isGrokChatHistory(want) {
		t.Fatalf("%q must be recognised as a Grok transcript", want)
	}
}
