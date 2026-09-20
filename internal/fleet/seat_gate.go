// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import "sync"

// SeatGate is the per-agent send/reap admission lock (🎯T627.4).
//
// A canonical mux/MCP send and the idle sweep must not interleave on the
// same seat: either the send is admitted and the sweep skips, or the sweep
// holds the seat, Stop runs, and the waiting send rehydrates afterward.
type SeatGate struct {
	mu    sync.Mutex
	seats map[string]*seatSlot
}

type seatSlot struct {
	senders int
	reaping bool
	wait    *sync.Cond
}

func (g *SeatGate) slotLocked(id string) *seatSlot {
	if g.seats == nil {
		g.seats = map[string]*seatSlot{}
	}
	s, ok := g.seats[id]
	if !ok {
		s = &seatSlot{}
		s.wait = sync.NewCond(&g.mu)
		g.seats[id] = s
	}
	return s
}

func (g *SeatGate) dropLocked(id string, s *seatSlot) {
	if s.senders == 0 && !s.reaping {
		delete(g.seats, id)
	}
}

// BeginSend admits a send. It waits if a reap is in progress so the
// caller can rehydrate after Stop. The returned function releases the
// admission; it is safe to call once.
func (g *SeatGate) BeginSend(id string) func() {
	if g == nil || id == "" {
		return func() {}
	}
	g.mu.Lock()
	s := g.slotLocked(id)
	for s.reaping {
		s.wait.Wait()
	}
	s.senders++
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			s.senders--
			if s.senders < 0 {
				s.senders = 0
			}
			s.wait.Broadcast()
			g.dropLocked(id, s)
		})
	}
}

// TryBeginReap admits a reap only when no send is admitted. ok is false
// when a send holds the seat or another reap is already running.
func (g *SeatGate) TryBeginReap(id string) (func(), bool) {
	if g == nil || id == "" {
		return func() {}, false
	}
	g.mu.Lock()
	s := g.slotLocked(id)
	if s.senders > 0 || s.reaping {
		g.mu.Unlock()
		return nil, false
	}
	s.reaping = true
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			s.reaping = false
			s.wait.Broadcast()
			g.dropLocked(id, s)
		})
	}, true
}

// Sending reports whether at least one send currently holds id.
func (g *SeatGate) Sending(id string) bool {
	if g == nil || id == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.seats[id]
	return ok && s.senders > 0
}
