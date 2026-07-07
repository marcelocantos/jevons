// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package thread

import (
	"path/filepath"
	"testing"
	"time"

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

func TestDeriveStatus(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-1 * time.Minute)
	old := now.Add(-30 * time.Minute)

	tests := []struct {
		name string
		in   StatusInput
		want State
	}{
		{
			name: "externally active wins over everything",
			in: StatusInput{
				ExternallyActive: true,
				Entries: []transcript.Entry{
					{Type: "assistant", Role: "assistant", Text: "done", StopReason: "end_turn", Timestamp: old},
				},
				Now: now,
			},
			want: StateActive,
		},
		{
			name: "no entries is idle",
			in:   StatusInput{Now: now},
			want: StateIdle,
		},
		{
			name: "assistant tool_use is working",
			in: StatusInput{
				Entries: []transcript.Entry{
					{Type: "user", Role: "user", Text: "go", IsUserTurn: true, Timestamp: recent},
					{Type: "assistant", Role: "assistant", HasToolUse: true, Timestamp: recent},
				},
				Now: now,
			},
			want: StateWorking,
		},
		{
			name: "assistant with no terminal stop_reason is working",
			in: StatusInput{
				Entries: []transcript.Entry{
					{Type: "assistant", Role: "assistant", Text: "thinking", Timestamp: recent},
				},
				Now: now,
			},
			want: StateWorking,
		},
		{
			name: "recent unanswered prompt is working",
			in: StatusInput{
				Entries: []transcript.Entry{
					{Type: "user", Role: "user", Text: "please fix", IsUserTurn: true, Timestamp: recent},
				},
				Now: now,
			},
			want: StateWorking,
		},
		{
			name: "stale unanswered prompt is blocked",
			in: StatusInput{
				Entries: []transcript.Entry{
					{Type: "user", Role: "user", Text: "please fix", IsUserTurn: true, Timestamp: old},
				},
				Now: now,
			},
			want: StateBlocked,
		},
		{
			name: "recent concluded turn is done",
			in: StatusInput{
				Entries: []transcript.Entry{
					{Type: "user", Role: "user", Text: "go", IsUserTurn: true, Timestamp: recent},
					{Type: "assistant", Role: "assistant", Text: "all set", StopReason: "end_turn", Timestamp: recent},
				},
				Now: now,
			},
			want: StateDone,
		},
		{
			name: "stale concluded turn is idle",
			in: StatusInput{
				Entries: []transcript.Entry{
					{Type: "assistant", Role: "assistant", Text: "all set", StopReason: "end_turn", Timestamp: old},
				},
				Now: now,
			},
			want: StateIdle,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveStatus(tc.in)
			if got.State != tc.want {
				t.Fatalf("state = %q, want %q (summary: %q)", got.State, tc.want, got.Summary)
			}
			if got.Summary == "" {
				t.Fatal("summary should never be empty")
			}
		})
	}
}
