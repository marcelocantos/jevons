// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestT870OverseerSequenceIsOneQuery(t *testing.T) {
	dir := t.TempDir()
	const session = "01a0c7fc-eacc-7703-9dd2-8fdb23e3783c"
	rows := []Turn{
		restartRow("2026-09-26T01:37:34.000Z", session, "host restarted at 11:37:34"),
		restartRow("2026-09-26T01:37:59.000Z", session, "host restarted at 11:37:59"),
		planRow("2026-09-26T01:55:00.000Z", session, "sentinel", "[event: sentinel] idle:mm2-t65-keys-doors", 40, 180),
		planRow("2026-09-26T02:01:00.000Z", session, "agent-forward", "[Agent mm2-t65-keys-doors responded] I'll inspect", 30, 200),
		planRow("2026-09-26T02:10:00.000Z", session, "impatience", "Impatience incident closed — jevons", 20, 160),
		{
			TS: "2026-09-26T02:24:34.000Z", Seat: "jevons", Type: "turn",
			TurnID: "fat", SessionID: session, Cause: "sentinel",
			CauseDetail: "[event: sentinel] idle:mm2-t65-keys-doors",
			StartedAt:   "2026-09-26T02:24:29.000Z", EndedAt: "2026-09-26T02:24:34.000Z",
			Stop: "end_turn", ToolCalls: 0, Deltas: 1063, Chars: 4314,
		},
	}
	var b strings.Builder
	for _, row := range rows {
		b.Write(mustTurnLine(t, row))
		b.WriteByte('\n')
	}
	// A turn_end snapshot that mentions the same causes must not become
	// extra rows, and it does not have to be valid JSON past the type.
	b.WriteString(`{"ts":"2026-09-26T02:24:34.000Z","seat":"jevons","type":"turn_end","snapshot":`)
	b.WriteString(strings.Repeat(`ONLY-IN-SNAPSHOT sentinel agent-forward impatience `, 4000))
	b.WriteByte('\n')
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-26.log"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := QueryTurns(dir, TurnQuery{Seat: "jevons"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(rows) {
		t.Fatalf("one query returned %d rows, want %d", len(got), len(rows))
	}
	if got[0].Cause != "restart-nudge" || got[0].Resume != "launched" ||
		got[1].Cause != "restart-nudge" || got[1].Resume != "launched" ||
		got[0].SessionID != got[1].SessionID || got[0].SessionID != session {
		t.Fatalf("restart rows = %+v %+v", got[0], got[1])
	}
	seen := map[string]bool{}
	for _, row := range got[2 : len(got)-1] {
		if row.ToolCalls != 0 {
			t.Fatalf("plan-only row called tools: %+v", row)
		}
		seen[row.Cause] = true
	}
	for _, cause := range []string{"sentinel", "agent-forward", "impatience"} {
		if !seen[cause] {
			t.Fatalf("plan-only causes = %v, missing %s", seen, cause)
		}
	}
	fat := got[len(got)-1]
	if fat.ToolCalls != 0 || fat.Chars < 1000 || fat.Deltas < 1000 {
		t.Fatalf("fat row = %+v, want thousands of chars and deltas and no tools", fat)
	}
	if fat.Chars != 4314 || fat.Deltas != 1063 {
		t.Fatalf("fat row = chars %d deltas %d", fat.Chars, fat.Deltas)
	}
	for _, row := range got {
		if strings.Contains(row.CauseDetail, "ONLY-IN-SNAPSHOT") || row.TurnID == "" || row.Stop == "" {
			t.Fatalf("row is not a digest: %+v", row)
		}
	}
}

func TestT870StopTokenIsNotTranscriptText(t *testing.T) {
	if got := VisibleAssistantText("I'll inspect <|eos|>"); got != "I'll inspect " {
		t.Fatalf("visible = %q", got)
	}
	if got := VisibleAssistantText("<|eos|>"); got != "" {
		t.Fatalf("bare token visible = %q", got)
	}
	lines := AsJSONL([]Record{
		{Type: "text", Text: "<|eos|>", Seat: "jevons"},
		{Type: "text", Text: "plan <|eos|>", Seat: "jevons"},
	})
	if strings.Contains(string(lines), "<|eos|>") {
		t.Fatalf("jsonl kept the stop token: %s", lines)
	}
	if !strings.Contains(string(lines), "plan ") {
		t.Fatalf("jsonl dropped the real text: %s", lines)
	}
}

func TestLineTypeStopsBeforeSnapshot(t *testing.T) {
	prefix := []byte(`{"ts":"2026-09-26T02:24:34.000Z","seat":"jevons","type":"turn_end","snapshot":`)
	if got := LineType(prefix); got != "turn_end" {
		t.Fatalf("LineType = %q", got)
	}
	if got := LineType([]byte(`{"ts":"t","seat":"jevons","type":"turn","cause":"owner"}`)); got != "turn" {
		t.Fatalf("digest type = %q", got)
	}
}

func restartRow(ts, session, detail string) Turn {
	return Turn{
		TS: ts, Seat: "jevons", Type: "turn",
		TurnID: "nudge-" + ts, SessionID: session,
		Cause: "restart-nudge", CauseDetail: detail, Resume: "launched",
		StartedAt: ts, EndedAt: ts, Stop: "end_turn",
	}
}

func planRow(ts, session, cause, detail string, deltas, chars int) Turn {
	return Turn{
		TS: ts, Seat: "jevons", Type: "turn",
		TurnID: cause + "-" + ts, SessionID: session,
		Cause: cause, CauseDetail: detail,
		StartedAt: ts, EndedAt: ts, Stop: "end_turn",
		ToolCalls: 0, Deltas: deltas, Chars: chars,
	}
}

func mustTurnLine(t *testing.T, row Turn) []byte {
	t.Helper()
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
