// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T1169: a fleet notify drain journals one machine-turn agent_note before
// the provider turn, with sender names and a count (not the payload).
func TestT1169FleetDrainJournalsMachineTurnBeforeSend(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dir := t.TempDir()
	s := New("test", dir)
	s.SetStateDB(db)
	clog, err := chatlog.Open(dir + "/chat.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	s.SetChatLog(clog)
	name := s.overseerAgentName()

	busy := true
	var delivered []string
	s.notifySender = func(text string) error {
		if busy {
			return fmt.Errorf("grok acp: prompt already in flight")
		}
		delivered = append(delivered, text)
		return nil
	}

	noteA := agentNote("jv-t1169-a", "finish-report body A")
	noteB := agentNote("jv-t1169-b", "finish-report body B")
	if err := s.SendToOverseer(noteA); err != nil {
		t.Fatal(err)
	}
	if err := s.SendToOverseer(noteB); err != nil {
		t.Fatal(err)
	}
	if len(s.notifyQueue) != 2 {
		t.Fatalf("want both reports queued while busy, got depth %d", len(s.notifyQueue))
	}
	busy = false
	s.drainOverseerNotes()
	if len(delivered) != 1 {
		t.Fatalf("want one fleet drain, got %d deliveries", len(delivered))
	}

	n := s.statedbN(name)
	rows := s.statedbRange(name, 1, n+1)
	var machineNoteIdx int
	var machineNoteBody string
	for _, ev := range rows {
		if ev.Type != "agent_note" {
			continue
		}
		var m map[string]any
		body := string(ev.Body)
		if json.Unmarshal(ev.Body, &m) != nil {
			continue
		}
		notice, _ := m["notice"].(map[string]any)
		if notice == nil {
			continue
		}
		if notice["kind"] == noticeKindMachineTurn {
			machineNoteIdx = ev.Index
			machineNoteBody = body
		}
	}
	if machineNoteIdx == 0 {
		t.Fatalf("no machine-turn agent_note in statedb (%d rows)", len(rows))
	}
	if !strings.Contains(machineNoteBody, "jv-t1169-a") || !strings.Contains(machineNoteBody, "jv-t1169-b") {
		t.Fatalf("summary must name senders, got %s", machineNoteBody)
	}
	if !strings.Contains(machineNoteBody, "2 machine report") {
		t.Fatalf("summary must carry count, got %s", machineNoteBody)
	}
	if strings.Contains(machineNoteBody, "finish-report") {
		t.Fatalf("summary must not carry payload, got %s", machineNoteBody)
	}

	// TailStart counts user rows only — the boundary note is not a user turn.
	s.persistChatLine(chatUserEcho("owner seed for tail"))
	loBefore := s.statedbTailStart(name, 1)
	s.persistChatLine(`{"type":"agent_note","text":"other","notice":{"kind":"machine-turn"}}`)
	loAfter := s.statedbTailStart(name, 1)
	if loBefore != loAfter {
		t.Fatalf("machine-turn agent_note shifted TailStart: before=%d after=%d", loBefore, loAfter)
	}
}

func TestT1169OwnerDrainSkipsMachineTurnNote(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New("test", t.TempDir())
	s.SetStateDB(db)
	s.notifySender = func(text string) error { return nil }

	_ = s.SendToOverseer(userTurnPrefix + "owner only")
	n := s.statedbN(s.overseerAgentName())
	for _, ev := range s.statedbRange(s.overseerAgentName(), 1, n+1) {
		if ev.Type == "agent_note" && strings.Contains(string(ev.Body), noticeKindMachineTurn) {
			t.Fatalf("owner drain must not journal machine-turn: %s", string(ev.Body))
		}
	}
}
