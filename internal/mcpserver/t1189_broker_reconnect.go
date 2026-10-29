// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/seatstop"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

// 🎯T1189 — after a Claudia broker restart/relaunch while jevonsd keeps
// running, stale registry handles can still read alive while every send
// answers `claude process not running` and /api/agents still shows idle.
// On 2026-10-22 a production broker bounce relaunched 77 seats; ~2 minutes
// of sends to idle rows failed until jevonsd itself restarted.
//
// T1110's relink sweep and T925's reattach loop eventually repair the
// fleet, but both are throttled and miss the locally-alive stale-handle
// shape until reconcile catches up. This package half notices broker
// recovery immediately and re-adopts every registered seat the broker
// still runs; the send path re-seats once on process_not_running.

const brokerReconnectPoll = time.Second

// sweepAfterBrokerReconnect is a test seam over SweepAfterBrokerReconnect.
var sweepAfterBrokerReconnect = (*Server).SweepAfterBrokerReconnect

// BrokerReconnectWatchArgs configures the broker-up watcher started at boot.
type BrokerReconnectWatchArgs struct {
	Server *Server
	// OnAgentsChanged runs after a reconnect sweep touched seats (UI refresh).
	OnAgentsChanged func()
	// BrokerUp is a seam over claudia.BrokerAvailable; nil uses the real probe.
	BrokerUp func() bool
}

// StartBrokerReconnectWatch polls for broker recovery and re-attaches seats
// without waiting for jevonsd's own restart (acceptance 1).
func StartBrokerReconnectWatch(ctx context.Context, args BrokerReconnectWatchArgs) {
	if args.Server == nil || args.Server.registry == nil {
		return
	}
	up := args.BrokerUp
	if up == nil {
		up = claudia.BrokerAvailable
	}
	wasUp := up()
	tick := time.NewTicker(brokerReconnectPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			nowUp := up()
			if !wasUp && nowUp {
				slog.Info("broker reconnect: control plane back, re-attaching fleet seats")
				if n := sweepAfterBrokerReconnect(args.Server, ctx); n > 0 && args.OnAgentsChanged != nil {
					args.OnAgentsChanged()
				}
			}
			wasUp = nowUp
		}
	}
}

// SweepAfterBrokerReconnect is the immediate, unthrottled re-attach pass after
// the broker comes back. It returns how many seat names were touched.
func (s *Server) SweepAfterBrokerReconnect(ctx context.Context) int {
	if s == nil || s.registry == nil {
		return 0
	}
	resetUnownedRelinkSweepThrottle()
	var touched []string
	touched = append(touched, s.reownOrStopUnownedSeats(readBrokerSeats())...)
	touched = append(touched, s.ReattachRunningSeats()...)
	touched = append(touched, s.confirmAliveSeatGrants(ctx)...)
	if len(touched) > 0 {
		slog.Info("broker reconnect: re-attached seats", "count", len(touched), "names", touched)
	}
	return len(touched)
}

// confirmAliveSeatGrants re-probes every locally-alive seat the broker still
// runs so a stale handle is replaced before the owner's next send.
func (s *Server) confirmAliveSeatGrants(ctx context.Context) []string {
	broker := readBrokerSeats()
	if broker == nil {
		return nil
	}
	observed := s.observeBrokerSeats(broker)
	intent := s.fleetIntent()
	var out []string
	for _, name := range s.allRegisteredSeatNames() {
		if fleetintent.StoodDown(intent.AgentState(name)) || s.reattachBlocked(name) {
			continue
		}
		if !s.localSeatAlive(name) {
			continue
		}
		seat, ok := observed[name]
		if !ok || seat.BrokerAlive != seatstate.Yes {
			continue
		}
		proc := s.registry.Get(name)
		if proc == nil {
			continue
		}
		fresh, err := upgrade.ConfirmBrokerGrant(ctx, name, proc)
		if err != nil {
			s.stopSeatBrokerReconnectFailed(name, err)
			out = append(out, name)
			continue
		}
		if fresh != nil && fresh != proc {
			s.EnsureAgentEventsWired(name)
			seatstate.ObserveRegistrySeat(s.registry, name)
			s.noteRevive(name, "broker_reconnect", "", nil)
			out = append(out, name)
		}
	}
	return out
}

func (s *Server) stopSeatBrokerReconnectFailed(name string, err error) {
	if s == nil || s.registry == nil {
		return
	}
	s.registry.Stop(name)
	seatstate.ObserveStopped(s.registry, name)
	s.noteSeatStopRecord(seatstop.Record{
		Seat: name, Source: seatstop.SourceBroker,
		Reason: "broker_reconnect_reattach_failed", Detail: err.Error(),
	})
	slog.Warn("stopped seat after broker reconnect re-attach failed", "name", name, "err", err)
}

// deliverWithReadopt is readoptDeliver plus a single re-seat when the broker
// answers process_not_running for a row the registry still calls alive.
func (s *Server) deliverWithReadopt(name string, proc agentSender, op func(agentSender) error) error {
	err := readoptDeliver(name, proc, op)
	if err == nil || agenterr.ProviderCause(err) != agenterr.CauseProcessNotRunning {
		return err
	}
	fresh, rerr := s.reseatAfterProcessNotRunning(name, proc)
	if rerr != nil {
		return err
	}
	return readoptDeliver(name, fresh, op)
}

func (s *Server) reseatAfterProcessNotRunning(name string, stale agentSender) (agentSender, error) {
	if s == nil || s.registry == nil {
		return nil, agenterr.WithCause(agenterr.CauseProcessNotRunning, errors.New("agent registry not available"))
	}
	if _, err := s.registry.Adopt(name); err != nil {
		s.stopSeatBrokerReconnectFailed(name, err)
		return nil, err
	}
	seatstate.ObserveRegistrySeat(s.registry, name)
	proc := s.registry.Get(name)
	if proc == nil {
		err := agenterr.WithCause(agenterr.CauseProcessNotRunning, errors.New("re-adopt returned no handle"))
		s.stopSeatBrokerReconnectFailed(name, err)
		return nil, err
	}
	s.EnsureAgentEventsWired(name)
	s.noteRevive(name, "broker_reconnect", "process_not_running", nil)
	if staleAgent, ok := stale.(*claudia.Agent); ok && staleAgent != proc {
		_ = staleAgent.Detach()
	}
	return proc, nil
}
