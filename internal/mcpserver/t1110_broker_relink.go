// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/seatstop"

	"github.com/marcelocantos/claudia"
)

// 🎯T1110 — after a broker restart or relink, a seat this host still holds
// a live local handle for can end up with its broker grant unowned: the
// broker runs the seat alive, but no connection holds it, and every send
// fails `claude process not running`. On 2026-10-21 12:00-12:23 three work
// seats (jv-t1108-auto, jv-t1109-auto, jv-t1074.1-auto) sat exactly there
// for 10+ minutes while `jevons_agent_list` kept calling them `running`.
//
// The existing reattach sweep (🎯T925/🎯T884, fleet.ReattachRunningSeats)
// only reconsiders a seat this host believes is NOT alive — its candidate
// filter skips any name `alive()` already reports true for, which is
// exactly the shape above: the local handle never died, only the broker's
// ownership of it did. This file is the other half: a seat this host
// believes alive while the broker calls it unowned is re-adopted, and a
// re-adopt that fails stops the row with a named reason instead of
// leaving it to report `running` forever.

// unownedWhileAliveSeats lists registered seat names — durable and work
// alike, a relink is blind to role — that this host believes alive while
// the broker's own grant says unowned. A seat whose intent has stood it
// down is left to 🎯T984's stand-down sweep, which already owns that case.
func unownedWhileAliveSeats(names []string, localAlive func(string) bool,
	broker map[string]seatstate.State, intent fleetintent.Snapshot) []string {
	var out []string
	for _, name := range names {
		if fleetintent.StoodDown(intent.AgentState(name)) {
			continue
		}
		if localAlive == nil || !localAlive(name) {
			continue
		}
		seat, ok := broker[name]
		if !ok || seat.BrokerOwned != seatstate.No {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// allRegisteredSeatNames is every registered seat, including the overseer
// and every PO — unlike registeredSeatNames, which the stand-down sweep
// uses deliberately to exclude the overseer. A broker relink does not
// spare durable rows (acceptance 3): the 2026-09-28 🎯T884 incident this
// package's sibling cites was four POs the broker ran unowned.
func (s *Server) allRegisteredSeatNames() []string {
	if s == nil || s.registry == nil {
		return nil
	}
	var names []string
	for _, d := range s.registry.List() {
		if d.Name != "" {
			names = append(names, d.Name)
		}
	}
	return names
}

// unownedRelinkSweepInterval bounds how often the relink sweep reads the
// broker's seat list. Reconcile runs every few seconds; the broker read
// need not.
const unownedRelinkSweepInterval = 30 * time.Second

var unownedRelinkSweepLast struct {
	mu sync.Mutex
	at time.Time
}

// sweepUnownedWhileAliveSeats is the Reconcile-driven entry point
// (🎯T1110): throttled to unownedRelinkSweepInterval, like its 🎯T984
// sibling.
func (s *Server) sweepUnownedWhileAliveSeats() []string {
	if s == nil || s.registry == nil {
		return nil
	}
	unownedRelinkSweepLast.mu.Lock()
	if time.Since(unownedRelinkSweepLast.at) < unownedRelinkSweepInterval {
		unownedRelinkSweepLast.mu.Unlock()
		return nil
	}
	unownedRelinkSweepLast.at = time.Now()
	unownedRelinkSweepLast.mu.Unlock()
	return s.reownOrStopUnownedSeats(readBrokerSeats())
}

// reownOrStopUnownedSeats re-adopts (re-owns) every candidate's broker
// grant; a seat whose adopt fails is stopped with reason
// `broker_relink_unowned` rather than left running-but-unreachable.
func (s *Server) reownOrStopUnownedSeats(broker map[string]claudia.BrokerSeat) []string {
	observed := s.observeBrokerSeats(broker)
	found := unownedWhileAliveSeats(s.allRegisteredSeatNames(), s.localSeatAlive, observed, s.fleetIntent())
	for _, name := range found {
		if _, err := s.registry.Adopt(name); err != nil {
			s.registry.Stop(name)
			seatstate.ObserveStopped(s.registry, name)
			s.noteSeatStopRecord(seatstop.Record{
				Seat: name, Source: seatstop.SourceBroker,
				Reason: "broker_relink_unowned", Detail: err.Error(),
			})
			slog.Warn("stopped a seat the broker relink left unowned", "name", name, "adopt_err", err)
			continue
		}
		slog.Info("re-owned a seat the broker relink left unowned", "name", name)
	}
	return found
}
