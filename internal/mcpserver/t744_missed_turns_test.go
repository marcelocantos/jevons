// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T744 — a turn that ended while the daemon held no event sink (boot resume
// is serial; the wire pass runs after it) is recovered from the transcript,
// stored verbatim, and announced to the parent.

func t744Line(t *testing.T, at time.Time, stop, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":      "assistant",
		"timestamp": at.UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"stop_reason": stop,
			"content":     []any{map[string]any{"type": "text", "text": text}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func t744Transcript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestT744RecoversTurnEndedBeforeSinkAttached(t *testing.T) {
	s, po, _, reportDir := t690Server(t)
	now := time.Now()
	scout := t690ScoutReport()

	// A checkpoint was stored 20m ago; a later scout-report ended in the gap.
	if _, err := agentreport.Save(reportDir, t690Worker, "checkpoint", now.Add(-20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	path := t744Transcript(t,
		t744Line(t, now.Add(-30*time.Minute), "end_turn", "older than the stored checkpoint"),
		t744Line(t, now.Add(-3*time.Minute), "end_turn", scout),
	)

	if n := s.recoverMissedTurns(t690Worker, path, now); n != 1 {
		t.Fatalf("missed turns = %d, want 1 (the one after the stored checkpoint)", n)
	}

	// The store — what 🎯T597 phaseAdvanceHandoff reads — holds the envelope verbatim.
	rec, err := agentreport.Latest(reportDir, t690Worker)
	if err != nil || rec.Text != scout {
		t.Fatalf("Latest() = %q, %v; want the scout-report verbatim", rec.Text, err)
	}

	// The parent got the report AND a loud notice that the seat was unobserved.
	var all string
	for _, m := range po.sent {
		all += m + "\n"
	}
	if !strings.Contains(all, "Scout complete") {
		t.Fatalf("parent did not receive the recovered report: %v", po.sent)
	}
	// The parent's turn is in flight in this fixture, so the notice may sit in
	// its send queue (delivered at the next turn boundary) rather than in sent.
	noticeSent := strings.Contains(all, "🎯T744") && strings.Contains(all, "unobserved")
	if !noticeSent && s.pendingAgentSends(t690Parent) != 1 {
		t.Fatalf("parent got no missed-turn notice: sent=%v queued=%d", po.sent, s.pendingAgentSends(t690Parent))
	}

	// Idempotent: the recovered report is now the latest, so a second attach
	// (the sweep re-attaching) finds nothing new and does not re-announce.
	before, queued := len(po.sent), s.pendingAgentSends(t690Parent)
	if n := s.recoverMissedTurns(t690Worker, path, now); n != 0 ||
		len(po.sent) != before || s.pendingAgentSends(t690Parent) != queued {
		t.Fatalf("second recovery n=%d sent %d→%d queued %d→%d; want none",
			n, before, len(po.sent), queued, s.pendingAgentSends(t690Parent))
	}
}

func TestT744LeavesTurnsTheLiveSinkOwns(t *testing.T) {
	s, po, _, _ := t690Server(t)
	now := time.Now()
	path := t744Transcript(t,
		t744Line(t, now.Add(2*time.Second), "end_turn", "ended after attach — the sink's"),
		t744Line(t, now.Add(-MissedTurnLookback-time.Hour), "end_turn", "ancient, beyond lookback"),
	)
	if n := s.recoverMissedTurns(t690Worker, path, now); n != 0 {
		t.Fatalf("missed turns = %d, want 0", n)
	}
	if len(po.sent) != 0 || s.pendingAgentSends(t690Parent) != 0 {
		t.Fatalf("parent was messaged for turns the sink owns or that are ancient: %v", po.sent)
	}
}

func TestT744MidTurnStopIsNotATurnEnd(t *testing.T) {
	now := time.Now()
	path := t744Transcript(t,
		t744Line(t, now.Add(-time.Minute), "tool_use", "thinking out loud"),
		t744Line(t, now.Add(-30*time.Second), "end_turn", "the answer"),
	)
	got, err := scanMissedTurns(path, now.Add(-time.Hour), now)
	if err != nil || len(got) != 1 {
		t.Fatalf("scan = %v, %v; want one turn", got, err)
	}
	if !strings.Contains(got[0].Text, "thinking out loud") || !strings.Contains(got[0].Text, "the answer") {
		t.Fatalf("turn text %q must accumulate across the mid-turn pause, as the sink does", got[0].Text)
	}
}
