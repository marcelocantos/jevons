// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/seatactivity"
)

// 🎯T745 — a pane that is busy rendering is not a CLI that never started.
//
// THE SPECIMEN (2026-09-21T17:15:04Z, jv-t729-startup-stall): a repressure
// send failed "Agent CLI stalled on startup (startup_stall / no_composer)"
// and the last frame was that seat's own test-file diff. claudia's Send waits
// for an idle composer, a pane painting a diff has none, and
// tmuxagent.NotReadyReason answers no_composer for ANY frame without one. The
// daemon then nudged a healthy seat (running, working, transcript 2.5s old)
// on every ladder tick. 🎯T729 fixed the same conflation on the spawn path;
// this is the send path.
//
// WHAT SEPARATES THEM. Both lack a composer, so the frame cannot decide. A
// seat that is mid-work has a transcript that MOVED recently; a CLI that
// never started has not written one for this session. seatactivity is the
// 🎯T702 / 🎯T679.1 seam for exactly that, so this reads the same mtime.
//
// WHY A SLOW FIRST RENDER CANNOT SPOOF IT. A first render writes no session
// transcript until the first turn begins, so a CLI still starting reads
// Unknown/absent, never a fresh mtime. Two ways a fresh mtime could still
// belong to a starting pane, both closed:
//   - a resumed session keeps its old file: a seat bounce-reminted this run
//     (bounceReminted) is excluded, and the file's age must be within
//     busyPaneFreshWindow, so only a mover in the last two minutes counts;
//   - a named startup reason (splash, rc_connecting, settings_warning) is
//     positive evidence of startup and is never overridden. Only the bare
//     no_composer reason, with a non-blank frame (something IS drawn), is
//     eligible.
//
// WHICH WAY THIS ERRS. Toward "busy". A wedged pane whose transcript moved
// inside the window is held (message queued, drained on the turn boundary)
// instead of flagged a stall for up to busyPaneFreshWindow — that is the
// class 🎯T679.2 / 🎯T694 detect by transcript age, so the window ends where
// they begin. The alternative error nudges a working seat in a loop, which
// spends its turns every tick and is what the specimen was. A long silent
// tool call (>window with no transcript write) still reads as a stall; that
// is the old behaviour, kept because silence that long is indistinguishable
// from a wedge.
const busyPaneFreshWindow = 2 * time.Minute

// paneBusyRendering is the pure verdict: err is a startup stall that the
// seat's own transcript proves is not one.
func paneBusyRendering(err error, activity seatactivity.Reading, reminted bool) bool {
	if err == nil || reminted {
		return false
	}
	msg := err.Error()
	if agenterr.ClassifyText(msg) != agenterr.ClassStartupStall {
		return false
	}
	if agenterr.StallReason(msg) != "no_composer" || strings.TrimSpace(agenterr.LastFrame(msg)) == "" {
		return false
	}
	return activity.Verdict == seatactivity.VerdictKnown &&
		activity.Age >= 0 && activity.Age <= busyPaneFreshWindow
}

// sendStallIsBusyPane reads the seat's transcript and applies the verdict.
func (s *Server) sendStallIsBusyPane(name string, err error) bool {
	if s == nil || s.registry == nil || err == nil ||
		agenterr.ClassifyText(err.Error()) != agenterr.ClassStartupStall {
		return false
	}
	d := s.registry.Def(name)
	if d == nil {
		return false
	}
	reading := seatactivity.Lookup(seatactivity.Query{
		Name: d.Name, Provider: d.Provider, SessionID: d.SessionID,
		WorkDir: d.WorkDir, Roots: DefaultSessionRoots(),
	})
	return paneBusyRendering(err, reading, s.bounceReminted(name))
}
