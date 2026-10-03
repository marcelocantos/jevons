// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/staffops"
)

// 🎯T984: a seat's running state is checked against its intent and against
// the broker's own view. On 2026-10-01 three seats jevons-po had parked were
// started by every daemon boot and resumed by a broker restart, while this
// daemon's rows called them stopped. Nothing compared what ran with what was
// meant to run, and nothing read the broker's view at all; the owner found it
// on the panel.

// Seams over the broker's seat list, so hermetics never dial the owner's broker.
var (
	brokerSeatsAvailable = claudia.BrokerAvailable
	brokerSeatsList      = claudia.BrokerSeats
	brokerSeatStop       = claudia.StopBrokerSeat
)

// brokerSeatsTimeout bounds one read of the broker's seat list.
const brokerSeatsTimeout = 5 * time.Second

// stoodDownSweepInterval is how often the reconcile pass reads the broker's
// seat list. Reconcile runs every few seconds; the broker read need not.
const stoodDownSweepInterval = 30 * time.Second

var stoodDownSweepLast struct {
	mu sync.Mutex
	at time.Time
}

// readBrokerSeats is the broker's view by seat name, or nil when there is no
// broker or it could not be read. Nil means unknown, never "nothing runs".
func readBrokerSeats() map[string]claudia.BrokerSeat {
	if !brokerSeatsAvailable() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), brokerSeatsTimeout)
	defer cancel()
	seats, err := brokerSeatsList(ctx)
	if err != nil {
		slog.Debug("broker seat list unavailable", "err", err)
		return nil
	}
	out := make(map[string]claudia.BrokerSeat, len(seats))
	for _, seat := range seats {
		out[seat.Name] = seat
	}
	return out
}

// seatAgainstIntent is a registered seat that runs although its intent stood
// it down.
type seatAgainstIntent struct {
	Name  string
	State fleetintent.State
	// Local is a live process this daemon holds; Broker is a live seat at
	// the broker that no connection holds.
	Local, Broker bool
}

// seatsAgainstIntent lists the registered seats running against their intent,
// sorted by name. broker may be nil (unknown): only local processes are judged.
func seatsAgainstIntent(names []string, localAlive func(string) bool, broker map[string]seatstate.State, intent fleetintent.Snapshot) []seatAgainstIntent {
	var out []seatAgainstIntent
	for _, name := range names {
		st := intent.AgentState(name)
		if !fleetintent.StoodDown(st) {
			continue
		}
		v := seatAgainstIntent{Name: name, State: st, Local: localAlive(name)}
		if seat, ok := broker[name]; ok && seat.BrokerAlive == seatstate.Yes && seat.BrokerOwned == seatstate.No {
			v.Broker = true
		}
		if v.Local || v.Broker {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// seatsDiverged lists registered seats this daemon holds no live process for
// while the broker reports them alive, sorted by name. Seats against their
// intent are left to seatsAgainstIntent.
func seatsDiverged(names []string, localAlive func(string) bool, broker map[string]seatstate.State, intent fleetintent.Snapshot) []string {
	var out []string
	for _, name := range names {
		if fleetintent.StoodDown(intent.AgentState(name)) || localAlive(name) {
			continue
		}
		if seat, ok := broker[name]; ok && seat.BrokerAlive == seatstate.Yes {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// registeredSeatNames is every registered seat except the overseer.
func (s *Server) registeredSeatNames() []string {
	overseer := s.overseerName()
	var names []string
	for _, d := range s.registry.List() {
		if d.Name != "" && d.Name != overseer {
			names = append(names, d.Name)
		}
	}
	return names
}

func (s *Server) localSeatAlive(name string) bool {
	proc := s.registry.Get(name)
	return proc != nil && (s.seatState(name).LocalAlive == seatstate.Yes)
}

// sweepStoodDownSeats stops every seat running against its intent: a process
// this daemon holds through the registry, an unowned broker seat through the
// broker. It is the harness's first response; the sentinel's intent_violation
// signal is the alarm when it does not hold.
func (s *Server) sweepStoodDownSeats() {
	if s == nil || s.registry == nil {
		return
	}
	stoodDownSweepLast.mu.Lock()
	if time.Since(stoodDownSweepLast.at) < stoodDownSweepInterval {
		stoodDownSweepLast.mu.Unlock()
		return
	}
	stoodDownSweepLast.at = time.Now()
	stoodDownSweepLast.mu.Unlock()
	s.stopSeatsAgainstIntent(readBrokerSeats())
}

func (s *Server) stopSeatsAgainstIntent(broker map[string]claudia.BrokerSeat) []seatAgainstIntent {
	observed := s.observeBrokerSeats(broker)
	found := seatsAgainstIntent(s.registeredSeatNames(), s.localSeatAlive, observed, s.fleetIntent())
	for _, v := range found {
		if v.Local {
			s.registry.Stop(v.Name)
			seatstate.ObserveStopped(s.registry, v.Name)
		} else if v.Broker {
			ctx, cancel := context.WithTimeout(context.Background(), brokerSeatsTimeout)
			err := brokerSeatStop(ctx, v.Name)
			cancel()
			if err != nil {
				slog.Warn("seat running against its intent was not stopped", "agent", v.Name, "intent", v.State, "err", err)
				continue
			}
		}
		slog.Info("stopped a seat running against its intent", "agent", v.Name, "intent", v.State, "local", v.Local, "broker", v.Broker)
		s.logLifecycle(compSentinel, "intent_stop", "ok", map[string]any{
			"name": v.Name, "intent": string(v.State), "local": v.Local, "broker": v.Broker,
		})
	}
	return found
}

// observeSeatViews marks the sentinel's agent observations with 🎯T984's two
// faults: a seat running against its intent, and a seat the broker runs while
// this daemon holds no process for it. Each must persist past grace before
// the policy repairs it.
func (s *Server) observeSeatViews(in *staffops.ObserveInput, intent fleetintent.Snapshot, now time.Time, grace time.Duration) {
	broker := s.observeBrokerSeats(readBrokerSeats())
	names := s.registeredSeatNames()
	marks := map[string]func(*staffops.AgentObs, bool){}
	for _, v := range seatsAgainstIntent(names, s.localSeatAlive, broker, intent) {
		where := "this daemon holds a live process"
		if !v.Local {
			where = "the broker runs it unowned"
		}
		detail := "intent is " + fleetintent.Describe(v.State) + " but " + where
		marks["intent:"+v.Name] = func(a *staffops.AgentObs, elapsed bool) { a.AgainstIntent, a.GraceElapsed = detail, elapsed }
	}
	for _, name := range seatsDiverged(names, s.localSeatAlive, broker, intent) {
		marks["diverge:"+name] = func(a *staffops.AgentObs, elapsed bool) {
			a.Diverged, a.GraceElapsed = "the broker runs this seat; this daemon holds no live process for it", elapsed
		}
	}
	rt := s.ensureSentinelRuntime(0)
	rt.mu.Lock()
	for sym := range rt.firstSeen {
		if strings.HasPrefix(sym, "intent:") || strings.HasPrefix(sym, "diverge:") {
			if _, still := marks[sym]; !still {
				delete(rt.firstSeen, sym)
			}
		}
	}
	elapsed := map[string]bool{}
	for sym := range marks {
		fs, seen := rt.firstSeen[sym]
		if !seen {
			rt.firstSeen[sym] = now
			fs = now
		}
		elapsed[sym] = now.Sub(fs) >= grace
	}
	rt.mu.Unlock()
	index := map[string]int{}
	for i, a := range in.Agents {
		index[a.Name] = i
	}
	for sym, mark := range marks {
		name := sym[strings.IndexByte(sym, ':')+1:]
		i, ok := index[name]
		if !ok {
			in.Agents = append(in.Agents, staffops.AgentObs{Name: name})
			i = len(in.Agents) - 1
			index[name] = i
		}
		mark(&in.Agents[i], elapsed[sym])
	}
}
