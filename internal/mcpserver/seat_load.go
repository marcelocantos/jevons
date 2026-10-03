// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/seatload"
	"github.com/marcelocantos/jevons/internal/seatstate"
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
		if proc == nil || !(s.seatState(def.Name).Alive == seatstate.Yes) {
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
	if proc == nil || !(s.seatState(name).Alive == seatstate.Yes) {
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
		if s.registry.Def(name) != nil && s.seatState(name).Alive != seatstate.No {
			continue
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

// seatLoadSources reads what each anchored seat is currently running and
// whether its own turn could still reach it. A seat with no prompt in
// flight has ended its turn: the 2026-09-20 specimen was in exactly that
// state, idle with its loops running, which is why nothing turn-scoped
// was ever going to clean up after it.
func (s *Server) seatLoadSources() []capacity.LoadSource {
	if s == nil {
		return nil
	}
	raw, err := s.seatLoadTracker().HostLoad()
	if err != nil {
		slog.Debug("seat load sources unavailable", "component", "seat_load", "err", err)
		return nil
	}
	out := make([]capacity.LoadSource, 0, len(raw))
	for _, src := range raw {
		// 🎯T766.2: read the authority rather than deriving idleness here.
		//
		// This used to default to idle and then ask the registry, so a seat
		// nothing was known about read as idle — and SeatIdle is what
		// authorises LoadTerminate to kill a process group. An absence
		// authorising a kill is the sharpest instance of the pattern the
		// census catalogued. Unknown now means not-idle: the governor may
		// starve a seat it cannot see, never terminate one.
		idle := false
		if st, ok := s.Seats().Get(src.Seat); ok && st.InFlight == seatstate.No {
			idle = true
		}
		out = append(out, capacity.LoadSource{
			Seat:       src.Seat,
			Procs:      src.Procs,
			CPUPercent: src.CPUPercent,
			Age:        src.Oldest,
			Unbounded:  src.Unbounded,
			Orphaned:   src.Orphaned,
			SeatIdle:   idle,
			Heaviest:   src.Heaviest,
		})
	}
	return out
}

// SweepSeatLoad is the governor's lever over load that is already running
// (🎯T708). 🎯T460 gates new panes; until now nothing could act on the load
// already there, so the governor read critical for forty minutes while one
// seat's loops starved the fleet, and named nothing.
//
// Every action is recorded where the overseer and the owner read it, not
// only in the daemon's own log: at critical a source nothing turn-scoped
// can reach is reaped and said so; one that a live turn could still bound
// is named to its seat instead of being reached into.
func (s *Server) SweepSeatLoad() []capacity.LoadAction {
	if s == nil {
		return nil
	}
	gov := s.CapacityGovernor()
	if gov == nil {
		return nil
	}
	acts := capacity.ActOnLoad(gov.Status().Assessment, s.seatLoadSources())
	s.applySeatLoadActions(acts)
	return acts
}

// applySeatLoadActions carries out what the policy decided.
func (s *Server) applySeatLoadActions(acts []capacity.LoadAction) {
	for _, act := range acts {
		line := capacity.FormatLoadAction(act)
		fields := map[string]any{
			"seat":      act.Source.Seat,
			"verdict":   string(act.Verdict),
			"audience":  string(act.Audience),
			"pressure":  act.Pressure.String(),
			"procs":     act.Source.Procs,
			"cpu":       act.Source.CPUPercent,
			"orphaned":  act.Source.Orphaned,
			"unbounded": act.Source.Unbounded,
			"seat_idle": act.Source.SeatIdle,
			"msg":       line,
		}
		if act.Verdict == capacity.LoadTerminate {
			res, err := s.seatLoadTracker().Reap(act.Source.Seat)
			if err != nil {
				fields["err"] = err.Error()
				s.logLifecycle("seat_load", "act", "error", fields)
				slog.Warn(line, "component", "seat_load", "err", err)
				continue
			}
			fields["terminated"] = len(res.Terminated)
			fields["survived"] = len(res.Survived)
			fields["reap"] = res.String()
		}
		s.logLifecycle("seat_load", "act", "ok", fields)
		slog.Warn(line, "component", "seat_load", "seat", act.Source.Seat, "verdict", string(act.Verdict))
	}
}

// hostLoadCritical reports whether the host's run queue — not the budget,
// not a provider cap — is the saturated dimension (🎯T708). A nil governor
// is not critical: unknown and saturated are different statements, and the
// stall bar must not go quiet because a reading is missing.
func (s *Server) hostLoadCritical() bool {
	if s == nil {
		return false
	}
	gov := s.CapacityGovernor()
	if gov == nil {
		return false
	}
	return capacity.HostLoadCritical(gov.Status().Assessment)
}

// forgetSeatLoad drops a seat's anchor without reaping — the seat is being
// handed over, not stopped.
func (s *Server) forgetSeatLoad(name string) {
	if s == nil {
		return
	}
	s.seatLoadTracker().Forget(name)
}
