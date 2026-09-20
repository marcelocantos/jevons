// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/thread"
)

func TestIdleEntryFromEventToolUseIsWorking(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	entries, err := liveStreamIdleEntries(idleObs{
		at:    at,
		entry: idleEntryFromEvent(claudia.Event{Type: "assistant", StopReason: "tool_use", Text: "calling"}, at),
	})
	if err != nil {
		t.Fatal(err)
	}
	st := thread.DeriveStatus(thread.StatusInput{
		Entries: entries, Now: at.Add(30 * time.Minute), ProcessUp: true,
	})
	if st.State != thread.StateWorking {
		t.Fatalf("tool wait derived %s, want working", st.State)
	}
}

func TestIdleEntryFromEventTerminalIdleWhenOld(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	entries, err := liveStreamIdleEntries(idleObs{
		at:    at,
		entry: idleEntryFromEvent(claudia.Event{Type: "assistant", StopReason: "end_turn", Text: "done"}, at),
	})
	if err != nil {
		t.Fatal(err)
	}
	st := thread.DeriveStatus(thread.StatusInput{
		Entries: entries, Now: at.Add(thread.DefaultIdleThreshold + time.Minute), ProcessUp: true,
	})
	if st.State != thread.StateIdle || st.LastActivity.IsZero() {
		t.Fatalf("old terminal derived %+v, want idle with activity", st)
	}
}

func TestLiveStreamIdleEntriesUnknownWithoutObservation(t *testing.T) {
	if _, err := liveStreamIdleEntries(idleObs{}); err == nil {
		t.Fatal("empty observation must not look idle")
	}
}

func TestCursorJSONLStillRejected(t *testing.T) {
	// Vestigial Claude-shaped files stay unreadable for Cursor; live-stream
	// observation is the GC source, not a colliding JSONL path.
	stored := &thread.Thread{ID: "aside", SessionID: "11111111-2222-3333-4444-555555555555"}
	def := &claudia.AgentDef{Name: "aside", Provider: claudia.ProviderCursor, SessionID: stored.SessionID}
	_, err := readIdleTranscript(stored, def, idleTranscriptHandle{stored.SessionID, "/tmp/old.jsonl"}, 40)
	if err == nil {
		t.Fatal("Cursor JSONL must not be treated as GC evidence")
	}
}
