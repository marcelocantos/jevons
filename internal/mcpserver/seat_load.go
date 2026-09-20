// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"

	"github.com/marcelocantos/jevons/internal/seatload"
)

// Seat-created background load dies with the seat (🎯T708).
//
// The daemon anchors each live seat's process group while its root process
// is alive, and reaps by that anchor when the seat stops. Anchoring early
// is the whole trick: once a seat's root exits, everything it detached is
// reparented to init, and a walk of parent links from a dead pid finds
// nothing at all. On 2026-09-20 that is precisely what happened — a seat
// went idle with sixteen `while :; do go test -race; done` shells still
// running, its parent was stopped, the overseer was stopped, and the owner
// had to be paged for a pkill.

// seatLoadTracker returns the process-group tracker, building it on first
// use. It is never nil, so callers need no guard.
func (s *Server) seatLoadTracker() *seatload.Tracker {
	if s == nil {
		return nil
	}
	s.seatLoadMu.Lock()
	defer s.seatLoadMu.Unlock()
	if s.seatLoad == nil {
		s.seatLoad = seatload.NewTracker()
	}
	return s.seatLoad
}

// TrackSeatLoad records an anchor for every live seat. It is cheap (one ps
// read per call plus a map write per seat) and idempotent, so the sweep
// that already walks the registry can call it without ceremony. Re-anchoring
// a seat replaces its anchor: a reminted seat is a new process.
func (s *Server) TrackSeatLoad() {
	if s == nil || s.registry == nil {
		return
	}
	tr := s.seatLoadTracker()
	for _, def := range s.registry.List() {
		proc := s.registry.Get(def.Name)
		if proc == nil || !proc.Alive() {
			continue
		}
		pid := proc.PID()
		if pid <= 1 {
			continue
		}
		if a, ok := tr.Anchor(def.Name); ok && a.PID == pid {
			continue
		}
		if _, err := tr.Track(def.Name, pid); err != nil {
			slog.Debug("seat load not anchored", "component", "seat_load", "seat", def.Name, "pid", pid, "err", err)
		}
	}
}

// trackSeatLoadFor anchors one seat by name, if it is alive. Called just
// before a stop, so a seat the sweep has not reached yet is still reaped
// with its descendants.
func (s *Server) trackSeatLoadFor(name string) {
	if s == nil || s.registry == nil {
		return
	}
	proc := s.registry.Get(name)
	if proc == nil || !proc.Alive() {
		return
	}
	if pid := proc.PID(); pid > 1 {
		_, _ = s.seatLoadTracker().Track(name, pid)
	}
}

// reapSeatLoad terminates the background work a seat leaves behind. It is
// called on the stop paths *before* the registry row goes, because the row
// is how the pid is found when the process is still alive.
//
// A reap that finds nothing is the normal case and says nothing. A reap
// that terminates something says so once, with the count and how many had
// already lost their parent — the number that says no turn-scoped cleanup
// could ever have reached them.
func (s *Server) reapSeatLoad(name string) seatload.Result {
	if s == nil {
		return seatload.Result{Seat: name}
	}
	s.trackSeatLoadFor(name)
	res, err := s.seatLoadTracker().Reap(name)
	if err != nil {
		slog.Warn("seat load reap failed", "component", "seat_load", "seat", name, "err", err)
		return res
	}
	if res.Any() {
		level := slog.LevelInfo
		if len(res.Survived) > 0 {
			level = slog.LevelWarn
		}
		slog.Log(nil, level, res.String(), "component", "seat_load", "seat", name,
			"terminated", len(res.Terminated), "survived", len(res.Survived), "orphaned", res.Orphaned)
	}
	return res
}

// ReapLostSeats reaps the background work of every anchored seat that is
// no longer running — the row has left the registry, or the process is not
// alive any more. This is the clause the specimen needed: the seat was not
// killed and not reaped, it simply went idle and then away, and nothing
// turn-scoped was ever going to reach its loops. Anchoring is what makes
// the reap possible after the fact; this is the sweep that uses it.
//
// It covers removal paths that do not run through the kill helpers at all,
// including ones in other packages, because it asks the registry what is
// still there rather than asking each caller to remember.
func (s *Server) ReapLostSeats() []seatload.Result {
	if s == nil || s.registry == nil {
		return nil
	}
	tr := s.seatLoadTracker()
	var out []seatload.Result
	for _, name := range tr.Seats() {
		if def := s.registry.Def(name); def != nil {
			if proc := s.registry.Get(name); proc != nil && proc.Alive() {
				continue
			}
		}
		res, err := tr.Reap(name)
		if err != nil {
			slog.Warn("lost seat load reap failed", "component", "seat_load", "seat", name, "err", err)
			continue
		}
		if res.Any() {
			slog.Warn(res.String(), "component", "seat_load", "seat", name,
				"terminated", len(res.Terminated), "survived", len(res.Survived), "orphaned", res.Orphaned)
			out = append(out, res)
		}
	}
	return out
}

// forgetSeatLoad drops a seat's anchor without reaping — the seat is being
// handed over, not stopped.
func (s *Server) forgetSeatLoad(name string) {
	if s == nil {
		return
	}
	s.seatLoadTracker().Forget(name)
}
