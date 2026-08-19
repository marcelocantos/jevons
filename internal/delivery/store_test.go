// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package delivery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordAndLookupSurviveCompactionShapedWipe(t *testing.T) {
	dir := t.TempDir()
	const agent = "jv-t417-ceiling"
	payload := "endorsement: T416 send-turn-begin landed; please continue with clause 2 of 🎯T417."
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	rec, err := RecordPayload(dir, agent, "ab2f5b2b-session", payload,
		"transcript gained a user message carrying this payload", "begun", now)
	if err != nil {
		t.Fatalf("RecordPayload: %v", err)
	}
	if rec.ID == "" || rec.Needle == "" {
		t.Fatalf("empty record: %+v", rec)
	}

	// Compaction-shaped wipe: the transcript is gone / reduced to a handover
	// seed. The delivery store must still answer.
	transcript := filepath.Join(dir, "sessions", "ab2f5b2b.jsonl")
	_ = os.MkdirAll(filepath.Dir(transcript), 0o755)
	_ = os.WriteFile(transcript, []byte(`{"type":"user","message":{"content":"handover only"}}\n`), 0o644)

	ok, err := WasDelivered(dir, agent, payload)
	if err != nil {
		t.Fatalf("WasDelivered: %v", err)
	}
	if !ok {
		t.Fatal("confirmed delivery became unprovable after transcript wipe — store was rewritten or ignored")
	}
	got, found, err := LookupPayload(dir, agent, payload)
	if err != nil || !found {
		t.Fatalf("LookupPayload found=%v err=%v", found, err)
	}
	if got.SessionID != "ab2f5b2b-session" {
		t.Fatalf("session_id=%q", got.SessionID)
	}
	if got.Outcome != "begun" {
		t.Fatalf("outcome=%q", got.Outcome)
	}
}

func TestShortPayloadRefused(t *testing.T) {
	dir := t.TempDir()
	_, err := RecordPayload(dir, "a", "", "ok", "detail", "begun", time.Now())
	if err == nil {
		t.Fatal("short payload must refuse rather than record an ambiguous needle")
	}
}

func TestLookupMissIsNotError(t *testing.T) {
	dir := t.TempDir()
	ok, err := WasDelivered(dir, "ghost", "a payload long enough to form a distinctive needle here")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if ok {
		t.Fatal("empty store reported delivered")
	}
}

func TestUnsafeAgentNameRefused(t *testing.T) {
	dir := t.TempDir()
	_, err := RecordPayload(dir, "../escape", "", strings.Repeat("distinctive payload text ", 4), "", "", time.Now())
	if err == nil {
		t.Fatal("path-escaping agent name must be refused")
	}
}
