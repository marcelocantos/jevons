// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package thread

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/transcript"
)

const testSession = "11111111-1111-1111-1111-111111111111"

func TestStoreAdoptPersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "threads.json")

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	when := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	got, err := s.Adopt(AdoptArgs{
		SessionID:   testSession,
		WorkDir:     "/work/repo",
		Description: "the multimaze2 rebuild",
		Now:         when,
	})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if got.ID != testSession || got.Kind != KindAdopted || got.WorkDir != "/work/repo" {
		t.Fatalf("unexpected thread: %+v", got)
	}

	// "Never lose a thread": a fresh Store over the same file must see it.
	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}
	reloaded, ok := s2.Get(testSession)
	if !ok {
		t.Fatal("thread did not survive reload")
	}
	if reloaded.Description != "the multimaze2 rebuild" || !reloaded.CreatedAt.Equal(when) {
		t.Fatalf("reloaded thread lost fields: %+v", reloaded)
	}
}

func TestStoreAdoptIdempotentAndConflict(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "threads.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := s.Adopt(AdoptArgs{ID: "po", SessionID: testSession}); err != nil {
		t.Fatalf("first adopt: %v", err)
	}
	// Same id + same session → idempotent, no error.
	if _, err := s.Adopt(AdoptArgs{ID: "po", SessionID: testSession}); err != nil {
		t.Fatalf("idempotent re-adopt should not error: %v", err)
	}
	// Same id + different session → conflict.
	other := "22222222-2222-2222-2222-222222222222"
	if _, err := s.Adopt(AdoptArgs{ID: "po", SessionID: other}); err == nil {
		t.Fatal("expected conflict adopting a different session under the same id")
	}
}

func TestStoreRequiresSessionID(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "threads.json"))
	if _, err := s.Adopt(AdoptArgs{}); err == nil {
		t.Fatal("expected error adopting without a session id")
	}
}

// 🎯T766.2: state comes from the seat-state authority; the transcript
// supplies content only. Unknown is never rounded to idle.
func TestDeriveStatus(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	tail := []transcript.Entry{
		{Type: "assistant", Role: "assistant", Text: "done", StopReason: "end_turn", Timestamp: now.Add(-30 * time.Minute)},
	}
	seat := func(alive, inflight seatstate.Tri) seatstate.State {
		return seatstate.State{Alive: alive, InFlight: inflight}
	}
	tests := []struct {
		name string
		in   StatusInput
		want State
	}{
		{"externally active wins", StatusInput{ExternallyActive: true, Entries: tail, Now: now,
			Seat: seat(seatstate.Yes, seatstate.No), SeatKnown: true}, StateActive},
		{"unreported seat is unknown, not idle — even with a quiet tail", StatusInput{Entries: tail, Now: now}, StateUnknown},
		{"turn in flight is working", StatusInput{Now: now, Seat: seat(seatstate.Yes, seatstate.Yes), SeatKnown: true}, StateWorking},
		{"alive with no turn is idle", StatusInput{Now: now, Seat: seat(seatstate.Yes, seatstate.No), SeatKnown: true}, StateIdle},
		{"not alive is stopped", StatusInput{Now: now, Seat: seat(seatstate.No, seatstate.Unknown), SeatKnown: true}, StateStopped},
		{"known seat whose in-flight decayed is unknown", StatusInput{Now: now, Seat: seat(seatstate.Unknown, seatstate.Unknown), SeatKnown: true}, StateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveStatus(tt.in).State; got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
		})
	}
	if got := DeriveStatus(StatusInput{Entries: tail, Now: now}); got.Summary == "" || got.LastActivity.IsZero() {
		t.Fatalf("transcript content lost from the status: %+v", got)
	}
}

// 🎯T33: integrity findings surface on DeriveStatus for list/status.
func TestDeriveStatusIntegrityFlag(t *testing.T) {
	now := time.Now()
	half := "this is a long enough prompt body"
	dup := half + half
	st := DeriveStatus(StatusInput{
		Entries: []transcript.Entry{
			{Type: "user", Role: "user", IsUserTurn: true, Text: dup, Timestamp: now},
		},
		Now: now,
	})
	if len(st.Integrity) == 0 {
		t.Fatal("expected integrity issues on doubled user turn")
	}
	if !strings.Contains(st.Summary, "integrity") {
		t.Fatalf("summary should flag integrity: %q", st.Summary)
	}
}
