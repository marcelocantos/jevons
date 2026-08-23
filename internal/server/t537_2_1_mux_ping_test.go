// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"
	"time"
)

// React daily driver talks /ws/mux, not /ws/chat. Vanilla ping must still
// tick owner_health so a connected React tab is not ui_heartbeat_stale (🎯T537.2.1).
func TestMuxPingNotesOwnerUIHeartbeat(t *testing.T) {
	s, _, clk := ownerHealthServer(t, func(string) error { return nil })
	s.ownerMu.Lock()
	s.ownerHealth.uiHeartbeatAt = clk.Now().Add(-time.Hour)
	s.ownerMu.Unlock()

	buf := &replayBuf{}
	sess := &muxSession{send: make(chan []byte, 1), transcripts: map[string]struct{}{}}
	s.handleMuxRaw(t.Context(), buf, sess, []byte(`{"type":"ping"}`))

	s.ownerMu.Lock()
	got := s.ownerHealth.uiHeartbeatAt
	s.ownerMu.Unlock()
	if !got.Equal(clk.Now()) {
		t.Fatalf("uiHeartbeatAt=%v want clock now %v — mux ping must NoteOwnerUIHeartbeat", got, clk.Now())
	}
	if len(buf.frames) != 1 || buf.frames[0]["type"] != "pong" {
		t.Fatalf("pong frames=%v", buf.frames)
	}
}

func TestMuxPingNilConnStillNotesHeartbeat(t *testing.T) {
	s, _, clk := ownerHealthServer(t, func(string) error { return nil })
	s.ownerMu.Lock()
	s.ownerHealth.uiHeartbeatAt = time.Time{}
	s.ownerMu.Unlock()

	s.handleMuxRaw(t.Context(), nil, &muxSession{}, []byte("  {\"type\":\"ping\"}  "))

	s.ownerMu.Lock()
	got := s.ownerHealth.uiHeartbeatAt
	s.ownerMu.Unlock()
	if got.IsZero() || !got.Equal(clk.Now()) {
		t.Fatalf("trimmed mux ping must still tick heartbeat, got %v", got)
	}
}
