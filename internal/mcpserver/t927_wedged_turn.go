// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
	"github.com/marcelocantos/jevons/internal/spool"
)

// 🎯T927 — a turn that died with its host handle is cleared, not waited on.
//
// 2026-09-29: jevons-po (anthropic sidecar) was mid-turn at 10:34:36Z, about
// to call a host tool, when the connection that would have answered the call
// closed. The seat was re-attached on a new handle at 10:35:42, but inside the
// sidecar the turn was still awaiting that tool result, which nothing would
// ever send. Every later prompt was queued behind it: the sidecar said
// "accepted", the drain read that as a turn beginning and logged "drained one
// message", and flight stayed in_flight. For more than two hours the backlog
// sweep said "its turn is still in flight" every thirty seconds, the PO
// authored nothing, and about twenty-five messages went into a queue nobody
// was reading.
//
// The sidecar half of the fix is claudia's: a host tool call whose connection
// closes fails instead of hanging, and a host tool honours abort. This half is
// the daemon's, and it holds whatever the provider does:
//
//   - a re-attach while a turn is believed in flight is RECORDED. That turn
//     was observed on a handle that no longer exists; the new handle has
//     shown nothing of it yet.
//   - a turn believed in flight that shows no motion for WedgedTurnAfter
//     while messages wait is WEDGED: flagged on /api/agents and agent_list,
//     and the overseer is told once, naming the messages handed to the seat
//     since its last answer. A bare "accepted" is not motion.
//   - a wedged sidecar turn that began on a lost handle is CLEARED: the
//     daemon interrupts it once. The provider's abort ends the turn, the
//     terminal stop drains the queue, and the flight record stops claiming a
//     turn the daemon cannot see. Any other wedge is reported, not
//     interrupted: silence alone does not tell a long tool call from a dead
//     one, and interrupting a working seat on a guess fights 🎯T414.

// WedgedTurnAfter is how long a turn believed in flight may show no motion,
// with messages waiting behind it, before it is called wedged. Longer than
// StalledBacklogAfter: a stall is "still waiting", a wedge is "nothing is
// coming".
const WedgedTurnAfter = 15 * time.Minute

// WedgedTurn is what the daemon knows about one wedged turn.
type WedgedTurn struct {
	Since      time.Time // start of the silence
	Queued     int       // messages waiting in the daemon's queue
	HandedOver []string  // queue entries handed to the seat since its last answer
	LostHandle bool      // the turn began on a handle that was replaced
	Cleared    bool      // the daemon interrupted it
	ClearErr   string    // why the interrupt failed, if it did
}

// turnWedges is the per-seat bookkeeping behind WedgedTurn. Its lock is a
// leaf: no method calls out while holding it, so flight writers may feed it
// while they hold Server.mu.
type turnWedges struct {
	mu         sync.Mutex
	inflightAt map[string]time.Time
	// began counts turn beginnings per seat, so a re-attach can name the
	// turn it saw in flight rather than whichever one is in flight later.
	began      map[string]uint64
	motion     map[string]time.Time
	lostHandle map[string]time.Time
	handed     map[string][]string
	wedged     map[string]WedgedTurn
}

func (w *turnWedges) flightBegan(name string, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inflightAt == nil {
		w.inflightAt = map[string]time.Time{}
	}
	w.inflightAt[name] = at
	if w.began == nil {
		w.began = map[string]uint64{}
	}
	w.began[name]++
}

// turnAt names the turn believed in flight right now: its beginning count,
// and whether one is believed in flight at all.
func (w *turnWedges) turnAt(name string) (uint64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, inFlight := w.inflightAt[name]
	return w.began[name], inFlight
}

// moved records motion from the seat. Motion on the current handle means the
// turn is running there, so it is neither lost nor wedged.
func (w *turnWedges) moved(name string, at time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.motion == nil {
		w.motion = map[string]time.Time{}
	}
	w.motion[name] = at
	delete(w.lostHandle, name)
	delete(w.wedged, name)
}

func (w *turnWedges) handedOver(name, entryID string) {
	if entryID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.handed == nil {
		w.handed = map[string][]string{}
	}
	w.handed[name] = append(w.handed[name], entryID)
}

// lostHandleUnder records that the turn named by turn (from turnAt, taken at
// the re-attach) began on a replaced handle. It records nothing, and says so,
// when that turn has since ended or another has begun: a turn that began
// after the re-attach began on the new handle.
func (w *turnWedges) lostHandleUnder(name string, turn uint64, at time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, inFlight := w.inflightAt[name]; !inFlight || w.began[name] != turn {
		return false
	}
	if w.lostHandle == nil {
		w.lostHandle = map[string]time.Time{}
	}
	w.lostHandle[name] = at
	return true
}

// turnEnded forgets everything about the turn: an observed end is an answer.
func (w *turnWedges) turnEnded(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.inflightAt, name)
	delete(w.lostHandle, name)
	delete(w.handed, name)
	delete(w.wedged, name)
}

// quietSince is the start of the current silence: the latest of the turn
// beginning, the last motion, and the re-attach. Zero when none is known.
func (w *turnWedges) quietSince(name string) (time.Time, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var at time.Time
	for _, t := range []time.Time{w.inflightAt[name], w.motion[name], w.lostHandle[name]} {
		if t.After(at) {
			at = t
		}
	}
	_, lost := w.lostHandle[name]
	return at, lost
}

// mark records a wedge and reports whether it is new.
func (w *turnWedges) mark(name string, wt WedgedTurn) (WedgedTurn, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.wedged == nil {
		w.wedged = map[string]WedgedTurn{}
	}
	prev, had := w.wedged[name]
	if had {
		wt.Cleared = prev.Cleared
		wt.ClearErr = prev.ClearErr
	}
	wt.HandedOver = append([]string(nil), w.handed[name]...)
	w.wedged[name] = wt
	return wt, !had
}

func (w *turnWedges) setCleared(name string, errText string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wt, ok := w.wedged[name]
	if !ok {
		return
	}
	wt.Cleared = errText == ""
	wt.ClearErr = errText
	w.wedged[name] = wt
	if errText == "" {
		// The turn the lost handle ran is gone; what comes next is new.
		delete(w.lostHandle, name)
	}
}

func (w *turnWedges) get(name string) (WedgedTurn, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wt, ok := w.wedged[name]
	return wt, ok
}

func (w *turnWedges) forget(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.wedged, name)
}

// noteReattachedMidTurn is called whenever a new process for name is wired,
// with the turn that was believed in flight at that moment (turnAt, read
// before the new process was subscribed). If that turn is still the one
// believed in flight, the belief was formed on the handle this one replaces.
//
// 🎯T937: the turn is named at the attach, not read here. This runs on a
// goroutine, and a turn that begins between the attach and this call began on
// the new handle; reading flight here alone called it lost, and the sweep
// interrupted a seat that was working on the only handle it ever had.
//
// 🎯T963: a process attached while a launch for the seat is in flight is
// that launch's successor, not a replacement for a handle that lost the
// turn: a launch in progress is not an outage (🎯T426). It is neither
// warned about nor recorded as a lost handle.
func (s *Server) noteReattachedMidTurn(name string, turn uint64, inFlight, launching bool) {
	if s == nil || !inFlight || launching || s.flightState(name) != FlightInFlight {
		return
	}
	if !s.wedges.lostHandleUnder(name, turn, time.Now()) {
		return
	}
	slog.Warn("🎯T927 re-attached a seat whose turn is believed in flight",
		"component", "agent_send", "agent", name,
		"queued", s.pendingAgentSends(name),
		"detail", "the turn was observed on the handle this one replaces; it is cleared if nothing is heard of it")
}

// isSidecarSeat reports whether name runs on a claudia sidecar, whose abort
// ends a turn even when the host that owned it is gone.
func (s *Server) isSidecarSeat(name string) bool {
	if s == nil || s.registry == nil {
		return false
	}
	def := s.registry.Def(name)
	return def != nil && spool.SidecarProvider(string(def.Provider))
}

// reconcileWedgedTurn decides whether a backlog held behind a turn in flight
// is held behind a turn that is not running. It returns true when it has
// handled the backlog (reported it wedged, and cleared the turn where it can),
// so the caller does not also report an ordinary stall.
func (s *Server) reconcileWedgedTurn(b sendq.Backlog, now time.Time) bool {
	name := b.Agent
	quiet, lost := s.wedges.quietSince(name)
	if quiet.IsZero() {
		// This process never saw the turn begin; the queue's own age is the
		// only clock it has.
		quiet = b.Oldest
	}
	if now.Sub(quiet) < WedgedTurnAfter || b.OldestAge(now) < WedgedTurnAfter {
		return false
	}
	wt, fresh := s.wedges.mark(name, WedgedTurn{Since: quiet, Queued: b.Depth, LostHandle: lost})

	clear := lost && !wt.Cleared && wt.ClearErr == "" && s.isSidecarSeat(name)
	if clear {
		// 🎯T937: the abort's terminal stop may land before Interrupt
		// returns. It ends the turn, which forgets the wedge record and sets
		// flight idle, so what happens next is read from here, not the map.
		generation := s.terminalGeneration(name)
		errText := ""
		if proc, live := s.liveSender(name); !live {
			errText = "no live process to interrupt"
		} else if err := readoptDeliver(name, proc, func(a agentSender) error { return a.Interrupt() }); err != nil {
			errText = err.Error()
		}
		s.wedges.setCleared(name, errText)
		wt.Cleared = errText == ""
		wt.ClearErr = errText
		if errText == "" {
			// The daemon no longer knows of a running turn. The abort's
			// terminal stop drains the queue; if it never arrives, the next
			// sweep re-offers the head as it would for any unknown seat. If
			// it has already arrived, flight says so, and a turn the drain
			// has since begun is not unknown.
			s.clearFlightUnlessEnded(name, generation)
			slog.Warn("🎯T927 cleared a turn that did not survive its host handle",
				"component", "agent_send", "agent", name, "queued", b.Depth,
				"silent", now.Sub(quiet).Round(time.Second).String())
		} else {
			slog.Error("🎯T927 could not clear a wedged turn",
				"component", "agent_send", "agent", name, "err", errText)
		}
	}

	if !fresh && !clear {
		slog.Debug("🎯T927 wedged turn already announced",
			"component", "agent_send", "agent", name, "queued", b.Depth)
		return true
	}
	slog.Warn("🎯T927 turn wedged",
		"component", "agent_send", "agent", name, "queued", b.Depth,
		"silent", now.Sub(quiet).Round(time.Second).String(),
		"handed_over", len(wt.HandedOver), "lost_handle", wt.LostHandle,
		"cleared", wt.Cleared)
	s.notifyFleetHealth("wedged:"+name+":"+quiet.UTC().Format(time.RFC3339Nano), FormatWedgedTurnLine(name, wt, now))
	return true
}

// FormatWedgedTurnLine is the operator-facing account of a wedged turn,
// shared by the fleet-health notice, agent_list and /api/agents.
func FormatWedgedTurnLine(name string, wt WedgedTurn, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WEDGED %s: its turn has been believed in flight for %s with no output from the seat; %d message(s) wait in the daemon's queue.",
		name, now.Sub(wt.Since).Round(time.Second), wt.Queued)
	if n := len(wt.HandedOver); n > 0 {
		fmt.Fprintf(&b, " %d message(s) handed to it since its last answer are unanswered (%s).",
			n, strings.Join(wt.HandedOver, ", "))
	}
	switch {
	case wt.Cleared:
		b.WriteString(" The turn began on a host handle that was lost; the daemon interrupted it so the queue drains (🎯T927).")
	case wt.ClearErr != "":
		fmt.Fprintf(&b, " The turn began on a host handle that was lost; interrupting it failed (%s). Stop and start the seat to resume it.", wt.ClearErr)
	default:
		fmt.Fprintf(&b, " If its transcript confirms nothing is running, jevons_agent_send name=%q mode=interrupt clears it.", name)
	}
	return b.String()
}

// WedgedSeat answers /api/agents: the wedge line for name, if its turn is
// wedged. A seat whose flight is no longer in flight is not wedged, whatever
// was recorded — including one the daemon has just cleared.
func (s *Server) WedgedSeat(name string) (string, bool) {
	if s == nil {
		return "", false
	}
	wt, ok := s.wedges.get(name)
	if !ok {
		return "", false
	}
	if s.flightState(name) != FlightInFlight {
		s.wedges.forget(name)
		return "", false
	}
	return FormatWedgedTurnLine(name, wt, time.Now()), true
}
