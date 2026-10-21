// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/statedb"
)

func TestT952FleetHealthNoteReachesTheTranscriptOnce(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New("test", t.TempDir())
	s.SetStateDB(db)
	var sent []string
	fail := true
	s.notifySender = func(text string) error {
		if fail {
			return fmt.Errorf("overseer not running")
		}
		sent = append(sent, text)
		return nil
	}

	note := `[Fleet health] Stalled backlog on "t952-stall": 1 message(s) held by the daemon, the oldest waiting 10m25s, because it has no live process to deliver to.`
	if err := s.SendToOverseer(note); err != nil {
		t.Fatal(err)
	}
	if got := fleetHealthStallRows(t, db, s.overseerAgentName()); got != 1 {
		t.Fatalf("deferred admission rows=%d want 1", got)
	}
	if len(sent) != 0 {
		t.Fatalf("deferred notice was sent: %v", sent)
	}

	fail = false
	s.drainOverseerNotes()
	if len(sent) != 1 || !strings.Contains(sent[0], `Stalled backlog on "t952-stall"`) {
		t.Fatalf("drain sent=%v", sent)
	}
	if got := fleetHealthStallRows(t, db, s.overseerAgentName()); got != 1 {
		t.Fatalf("drain duplicated the transcript row: %d", got)
	}

	// The provider echo of the same prompt is not a second episode.
	s.DeliverOverseerEvent(claudia.Event{Type: "user", Text: note})
	if got := fleetHealthStallRows(t, db, s.overseerAgentName()); got != 1 {
		t.Fatalf("provider echo duplicated the transcript row: %d", got)
	}

	// A worker reply is not a fleet-health episode.
	s.HandleAgentEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	if err := s.SendToOverseer("[Agent worker responded]\nhello"); err != nil {
		t.Fatal(err)
	}
	if got := fleetHealthStallRows(t, db, s.overseerAgentName()); got != 1 {
		t.Fatalf("worker reply changed stall rows: %d", got)
	}
}

func fleetHealthStallRows(t *testing.T, db *statedb.Store, agent string) int {
	t.Helper()
	n, err := db.N(agent)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := db.Range(agent, 1, n+1)
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, ev := range evs {
		body := strings.ReplaceAll(string(ev.Body), `\"`, `"`)
		if !strings.Contains(body, `Stalled backlog on "t952-stall"`) {
			continue
		}
		if ev.Type != "agent_note" {
			t.Fatalf("stall row type=%q body=%s", ev.Type, ev.Body)
		}
		got++
	}
	return got
}
