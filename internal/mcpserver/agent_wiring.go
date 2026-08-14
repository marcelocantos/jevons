// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T426 — an agent's event stream is an INVARIANT, not a step on one path.
//
// Every control the daemon has over a running agent hangs off one wire:
// agentEventSink's terminal-stop branch. noteTurnEnded (the only writer of
// FlightIdle), drainAgentSendQueue (called from nowhere else), notify, and the
// 🎯T165 auto-reap all fire there and nowhere else. Detach that sink and the
// agent does not degrade — it goes dark, while its senders keep being told
// their messages are safely queued.
//
// The sink used to be attached by whoever happened to launch the process, and
// launch roads keep being added. 🎯T61 found the first gap: registry.StartAll
// at boot bypasses handleAgentStart, so a resumed worker's reply never reached
// the overseer, and WireRunningAgents was added as a boot-time patch. On
// 2026-08-10 the context-ceiling governor became the second road — it rotates
// an over-budget agent onto a fresh session through fleet.Claudia.Launch,
// which is registry-level and knows nothing about this package. jevons-po was
// compacted at 20:14:44, its successor ran a full turn and ended it at
// 20:18:43, and the daemon never saw it. Six messages stacked behind a flight
// record pinned at in_flight by a process that no longer existed.
//
// Two roads, one shape, and a third would have done it again. So the wiring
// stops being a step and becomes a state the daemon asserts:
//
//   - EnsureAgentEventsWired is idempotent per PROCESS OBJECT, so every launch
//     road can call it unconditionally and a road that calls it twice costs
//     nothing. It is what the four historical call sites now go through.
//   - fleet.Claudia carries a launch hook (SetLaunchHook), so compaction,
//     provider migration, thread launches and butler deliver-rehydrates are
//     covered at their shared chokepoint rather than one at a time.
//   - the fleet-wide pass is no longer a boot-time one-shot: the same body
//     runs as a standing sweep (StartAgentWireSweep). A launch road nobody
//     wired — road six — is repaired within one interval instead of never.
//     It found one on its first day: jv-t383-auto, two minutes after boot.
//
// The true chokepoint is claudia.Registry.Launch itself, which would catch
// StartAll and every future caller at the source. It is not taken here because
// claudia is a separate module consumed at a released version: a hook there
// needs a claudia release and a go.mod bump, which is a second repo and the
// Ship plane, while this outage is live on master (🎯T104). The sweep is the
// in-repo stand-in for that hook, and it is deliberately an assertion about
// the world rather than a promise about code paths.
//
// FAIL-LOUD, because the failure mode is silence (clause 3). Attaching a sink
// to a process that is ALIVE AND ALREADY REGISTERED means the daemon had lost
// that agent's event stream — a turn may have begun and ended unobserved. That
// is reported: a WARN naming the queue it was holding, and, when a sender is
// standing right there being told "queued", the truth in its reply. A launch
// is exempt: a brand-new process has no history to have missed.

// DefaultAgentWireInterval is how often the standing sweep re-asserts the
// invariant. Unhurried on purpose, like the ceiling governor it backstops: a
// dark agent is repaired in tens of seconds, and the sweep itself is a map
// walk over live processes, not a provider call.
const DefaultAgentWireInterval = 30 * time.Second

// agentProcResolver resolves the live process to wire for an agent, or nil
// when there is none to wire. Nil — the product path — reads the registry and
// requires the process to be alive. Test seam (SetProcResolver), matching the
// senderResolver / turnWitness pattern: the seam owns its own liveness
// judgement, so a fixture can drive the wiring with a bare claudia.Agent.
type agentProcResolver func(name string) *claudia.Agent

// wiredSink records which process object currently carries this daemon's
// event sink for an agent, and the token that removes it.
//
// Keyed on the process OBJECT rather than the agent name because that is what
// a rotation changes: after a compaction the name, the registry row and the
// workdir are all the same, and the only thing that tells the daemon it is
// looking at a different conversation is a different *claudia.Agent.
type wiredSink struct {
	proc  *claudia.Agent
	token int64
}

// SetProcResolver overrides how the wiring finds an agent's live process.
func (s *Server) SetProcResolver(fn agentProcResolver) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveProc = fn
}

// lookupAgentProc returns the process that should carry the sink for name.
func (s *Server) lookupAgentProc(name string) *claudia.Agent {
	s.mu.Lock()
	resolve := s.resolveProc
	s.mu.Unlock()
	if resolve != nil {
		return resolve(name)
	}
	if s.registry == nil {
		return nil
	}
	proc := s.registry.Get(name)
	if proc == nil || !proc.Alive() {
		return nil
	}
	return proc
}

// EnsureAgentEventsWired attaches this daemon's event sink to name's current
// process unless that exact process already carries it. Safe to call on every
// launch road, repeatedly.
//
// The bool answers "did this call attach the sink", which is the same question
// as "was this process dark until now" — and what it MEANS depends on who is
// asking. A caller that has just launched a process should ignore it: a new
// process has never been wired and never missed anything. A caller looking at
// an agent it believes was already running should treat true as a fault, and
// the two that do (the sweep and the queued-send reply) say so out loud.
func (s *Server) EnsureAgentEventsWired(name string) bool {
	if s == nil || strings.TrimSpace(name) == "" {
		return false
	}
	// The overseer has its own event stream via Server.AttachOverseer, which
	// owns the owner-chat journal. Attaching the fleet sink as well would
	// deliver every overseer turn to the overseer as a worker report.
	if s.isOverseerAgent(name) {
		return false
	}
	proc := s.lookupAgentProc(name)
	if proc == nil {
		return false
	}
	return s.attachAgentSink(name, proc)
}

// attachAgentSink is the wiring itself: subscribe unless this process object
// already holds the subscription, and drop a subscription left on the
// predecessor process so a pane that outlives its rotation cannot report turns
// under a name that has moved on.
//
// Guarded by wireMu rather than mu, and that is not incidental. Events are
// published while claudia holds the agent's own lock, and the sink reaches
// back into Server.mu (broadcastAgentEvent); taking mu here — before
// SubscribeEvents takes the agent lock — would close the cycle. wireMu is only
// ever taken by this function, and this function is never called under mu.
func (s *Server) attachAgentSink(name string, proc *claudia.Agent) bool {
	s.wireMu.Lock()
	defer s.wireMu.Unlock()
	if s.wiredSinks == nil {
		s.wiredSinks = map[string]wiredSink{}
	}
	prev, had := s.wiredSinks[name]
	if had && prev.proc == proc {
		return false
	}
	if had && prev.proc != nil {
		prev.proc.UnsubscribeEvents(prev.token)
	}
	token := proc.SubscribeEvents(s.agentEventSink(name))
	s.wiredSinks[name] = wiredSink{proc: proc, token: token}
	return true
}

// forgetAgentWiring drops the wiring record for a torn-down seat. Must not be
// called while holding Server.mu — see attachAgentSink on lock order.
func (s *Server) forgetAgentWiring(name string) {
	if s == nil {
		return
	}
	s.wireMu.Lock()
	defer s.wireMu.Unlock()
	delete(s.wiredSinks, name)
}

// wireFleet asserts the invariant across the whole fleet — every live
// registered agent except the overseer carries the sink — and returns the names
// it had to attach. Idempotent, so it is the body of both fleet-wide passes.
//
// It does not log. What an attach MEANS is the whole difference between its two
// callers, and only they know it (see EnsureAgentEventsWired's bool).
func (s *Server) wireFleet(overseerName string) []string {
	if s == nil || s.registry == nil {
		return nil
	}
	var attached []string
	for _, def := range s.registry.List() {
		if def.Name == overseerName {
			continue
		}
		if s.EnsureAgentEventsWired(def.Name) {
			attached = append(attached, def.Name)
		}
	}
	return attached
}

// WireRunningAgents is the boot-time pass after registry.StartAll (🎯T61).
//
// Every resumed agent attaches here, because at boot nothing has wired anything
// yet — this pass IS the wiring, not a repair of one. Reporting seventeen
// resumed workers as seventeen faults is how the one real fault goes unread:
// on 2026-08-15 the boot pass warned about all 17 agents at 08:27:47, and the
// sweep's genuine find two minutes later (jv-t383-auto, a launch road that does
// not wire) arrived looking exactly like the noise above it. So the boot pass
// states its count once, at INFO, and the WARN vocabulary stays scarce enough
// to mean something.
func (s *Server) WireRunningAgents(overseerName string) int {
	attached := s.wireFleet(overseerName)
	if len(attached) > 0 {
		slog.Info("🎯T61 wired event streams for agents resumed at boot",
			"count", len(attached))
	}
	return len(attached)
}

// sweepAgentWiring is the standing pass (🎯T426), and by then an attach is a
// fault: the daemon has been running, so an agent that was dark had a turn the
// daemon could have missed.
//
// Returns the number of agents whose stream was dark and has now been
// re-attached. In steady state that is zero; anything else is a launch road
// that does not wire, and it is logged as one.
func (s *Server) sweepAgentWiring(overseerName string) int {
	attached := s.wireFleet(overseerName)
	for _, name := range attached {
		// Loud, and with the cost named. A sweep that attaches a sink to an
		// already-running agent has found an agent the daemon could not see
		// turn boundaries for, and the queue depth is how much of the fleet's
		// traffic was stranded behind that blindness.
		if pending := s.pendingAgentSends(name); pending > 0 {
			slog.Warn("🎯T426 agent event stream was dark — messages held across an unobserved turn boundary",
				"agent", name, "queued", pending,
				"detail", "re-attached; the queue drains at this agent's next observed terminal stop")
		} else {
			slog.Warn("🎯T426 agent event stream was dark — re-attached",
				"agent", name,
				"detail", "a launch road did not wire this process; turn ends were not being observed")
		}
	}
	return len(attached)
}

// StartAgentWireSweep runs sweepAgentWiring on a ticker until ctx is done.
// This is the backstop for launch roads that do not yet call the hook — the
// reason 🎯T426 is not simply a fifth call site.
func StartAgentWireSweep(ctx context.Context, s *Server, overseerName string, interval time.Duration) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultAgentWireInterval
	}
	slog.Info("agent event wiring sweep", "interval", interval)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweepAgentWiring(overseerName)
			}
		}
	}()
}
