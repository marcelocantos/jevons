// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SeatWait is a frontier target whose worker could not be seated: a spawn
// for it was refused because no plan can take a new seat (🎯T980).
type SeatWait struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// seatWaits remembers the targets waiting for a seat, so the frontier play
// button can show pause and the reason instead of spinning. Play is only a
// nudge to prioritise a target: it never forces work onto plans that cannot
// take it (owner, 2026-10-01).
type seatWaits struct {
	mu sync.Mutex
	m  map[string]SeatWait
}

func (s *Server) noteSeatWait(target, reason string) {
	target = normalizeAgentTargetID(target)
	if s == nil || target == "" {
		return
	}
	s.seatWait.mu.Lock()
	defer s.seatWait.mu.Unlock()
	if s.seatWait.m == nil {
		s.seatWait.m = map[string]SeatWait{}
	}
	s.seatWait.m[target] = SeatWait{Reason: strings.TrimSpace(reason), At: time.Now()}
}

// ClearSeatWait forgets target's wait: a worker engaged it, or the owner
// stopped the request.
func (s *Server) ClearSeatWait(target string) {
	target = normalizeAgentTargetID(target)
	if s == nil || target == "" {
		return
	}
	s.seatWait.mu.Lock()
	defer s.seatWait.mu.Unlock()
	delete(s.seatWait.m, target)
}

// SeatWaits is a copy of the targets waiting for a seat.
func (s *Server) SeatWaits() map[string]SeatWait {
	out := map[string]SeatWait{}
	if s == nil {
		return out
	}
	s.seatWait.mu.Lock()
	defer s.seatWait.mu.Unlock()
	for k, v := range s.seatWait.m {
		out[k] = v
	}
	return out
}

// handleSeatWaits serves GET /api/seat-waits.
func (s *Server) handleSeatWaits(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.SeatWaits())
}
