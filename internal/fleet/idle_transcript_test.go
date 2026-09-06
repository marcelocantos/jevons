// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/thread"
)

type idleTranscriptHandle struct{ sid, path string }

func (h idleTranscriptHandle) SessionID() string { return h.sid }
func (h idleTranscriptHandle) JSONLPath() string { return h.path }

// The obsolete file is readable and genuinely idle in every refusal case.
// Omitting the production provider or identity guard therefore exposes it.
func TestIdleTranscriptRejectsForeignOrPredecessorHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"role":"assistant","stop_reason":"end_turn","content":"old completed turn"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const current = "11111111-2222-3333-4444-555555555555"
	const old = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	for _, tc := range []struct {
		name                     string
		provider                 claudia.Provider
		stored, registered, live string
		wantRead                 bool
	}{
		{"matching-Claude", claudia.ProviderClaude, current, current, current, true},
		{"Cursor-colliding-file", claudia.ProviderCursor, current, current, current, false},
		{"Codex-colliding-file", claudia.ProviderCodex, current, current, current, false},
		{"Grok-other-store", claudia.ProviderGrok, current, current, current, false},
		{"stored-predecessor", claudia.ProviderClaude, old, current, current, false},
		{"old-live-handle", claudia.ProviderClaude, current, current, old, false},
		{"registry-rotation", claudia.ProviderClaude, current, old, current, false},
		{"unknown-identity", claudia.ProviderClaude, "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := &thread.Thread{ID: "aside", Provider: "claude", SessionID: tc.stored}
			def := &claudia.AgentDef{Name: "aside", Provider: tc.provider, SessionID: tc.registered}
			entries, err := readIdleTranscript(stored, def, idleTranscriptHandle{tc.live, path}, 40)
			if !tc.wantRead {
				if err == nil || len(entries) != 0 {
					t.Fatalf("read unrelated idle history: %+v, %v", entries, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			status := thread.DeriveStatus(thread.StatusInput{Entries: entries, Now: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), ProcessUp: true})
			if status.State != thread.StateIdle || status.LastActivity.IsZero() {
				t.Fatalf("positive control not truly idle: %+v", status)
			}
		})
	}
}
