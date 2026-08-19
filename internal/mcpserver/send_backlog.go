// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T418 — WHAT HAPPENS TO A MESSAGE THE DAEMON ACCEPTED.
//
// 🎯T416 stopped the daemon pasting a message into a pane whose turn is
// running: the provider CLI's own queue is a store jevons can neither see nor
// replay, it merges silently with later sends, and it dies with the pane. The
// message is held in the daemon's queue instead — which was a map in this
// process's memory, so the loss moved rather than went away. jevonsd restarted
// three times on the day 🎯T416 was written and four times in one session on
// 2026-08-15, and every one of those bounces emptied a queue whose senders had
// been told "queued (N pending) … held by the daemon".
//
// The queue is now a directory (internal/sendq). This file is the half that
// makes durability mean something to an operator rather than only to the disk:
//
//   - what the daemon recovered is REPORTED on the way up, because a backlog
//     that survives silently is indistinguishable from one that was delivered;
//   - the queue is DRAINED without waiting for a turn boundary that may never
//     come again, because the boundary the queue was waiting for was consumed
//     by the very restart that saved it;
//   - a queue addressed to an agent that no longer exists is SURFACED and
//     dropped, not kept forever against a seat nobody will fill;
//   - a queue that stops moving is NAMED, with the age of its oldest entry,
//     because a counter only the daemon reads is where "queued" goes to die.
//
// The sweep is also the answer to clause 6: recovery must not bottom out in a
// human keystroke. Nothing here asks an unstuck agent to press Enter — the
// daemon re-offers the message itself on its own schedule, and where it cannot,
// it says so to the overseer rather than waiting to be noticed.

// StalledBacklogAfter is how long a queue may sit before the daemon calls it
// stalled rather than waiting. It is a REPORTING threshold and never a delivery
// one: nothing is dropped or re-sent because of it. Ten minutes is the same
// order as a turn that is genuinely long, so a working agent mid-task does not
// generate an alarm for the normal state of the world (🎯T426).
const StalledBacklogAfter = 10 * time.Minute

// sendQueue is the durable backlog, created memory-backed on first use so a
// Server with no state directory (a hermetic test, a daemon started without
// one) still has exactly one queue implementation.
func (s *Server) sendQueue() *sendq.Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentSendQ == nil {
		s.agentSendQ = sendq.NewStore("")
	}
	return s.agentSendQ
}

// SetSendQueueDir roots the backlog at <state_dir>/sendq. Called before the
// fleet starts, so a message accepted by the previous daemon is already
// readable when the first turn boundary arrives.
//
// A state directory that cannot be created is reported and the daemon keeps
// running with a memory-backed queue — refusing to start over an undeliverable
// backlog would trade a durability defect for an outage — but it is logged as
// an error, not a notice, because the fleet is then back to the behaviour this
// target exists to end.
func (s *Server) SetSendQueueDir(stateDir string) {
	stateDir = strings.TrimSpace(stateDir)
	if strings.HasPrefix(stateDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			stateDir = filepath.Join(home, stateDir[2:])
		}
	}
	if stateDir == "" {
		slog.Error("🎯T418 send queue is memory-backed: no state directory",
			"component", "agent_send",
			"detail", "a message accepted while an agent is busy will not survive a restart")
		return
	}
	dir := filepath.Join(stateDir, "sendq")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("🎯T418 send queue is memory-backed: state directory unusable",
			"component", "agent_send", "dir", dir, "err", err)
		return
	}
	s.mu.Lock()
	s.agentSendQ = sendq.NewStore(dir)
	s.mu.Unlock()
	slog.Info("send queue durable", "component", "agent_send", "dir", dir)
}

// ReportRecoveredBacklog says what the previous daemon left behind. Called once
// at startup, before the fleet is running, so the line that names a recovered
// backlog cannot be confused with one the running daemon accepted.
//
// It reports rather than delivers: the agents are not up yet. Delivery is
// SweepSendBacklogs, which runs once the fleet is alive.
func (s *Server) ReportRecoveredBacklog() {
	q := s.sendQueue()
	if !q.Durable() {
		return
	}
	backlogs, err := q.Backlogs()
	if err != nil {
		slog.Error("🎯T418 recovered backlog unreadable",
			"component", "agent_send", "dir", q.Dir(), "err", err)
		return
	}
	if len(backlogs) == 0 {
		return
	}
	now := time.Now()
	var lines []string
	total := 0
	for _, b := range backlogs {
		total += b.Depth
		lines = append(lines, b.Describe(now))
		slog.Info("🎯T418 recovered a queued message the last daemon accepted",
			"component", "agent_send",
			"agent", b.Agent,
			"queued", b.Depth,
			"oldest_age", b.OldestAge(now).Round(time.Second).String())
	}
	s.notifyFleetHealth(fmt.Sprintf(
		"Recovered %d queued message(s) the previous daemon had accepted but not delivered: %s. "+
			"They survived the restart on disk and are offered again at each agent's next turn boundary, "+
			"or reported here if they cannot be.",
		total, strings.Join(lines, "; ")))
}

// SweepSendBacklogs re-offers held messages and surfaces the ones it cannot
// deliver. Called from the fleet-health sweep, so it runs on the daemon's own
// schedule rather than on a turn boundary that may already have been missed.
//
// THE THREE ANSWERS, one per reason a queue is not moving:
//
//   - the agent is idle and alive → drain it here. This is the recovery the
//     restart destroyed: the boundary this queue was waiting for was consumed
//     by the bounce, and a queue that waits for the next one waits for an event
//     nothing will now generate.
//   - the agent is not in the registry at all → the seat is gone, so the
//     message has nowhere to be delivered. Report it with its payload size and
//     age, then drop it: a queue against a name nobody will fill is not pending.
//   - the agent is registered but busy or not running → leave it queued, and
//     say so once it has been waiting longer than StalledBacklogAfter.
func (s *Server) SweepSendBacklogs() {
	q := s.sendQueue()
	backlogs, err := q.Backlogs()
	if err != nil {
		slog.Error("🎯T418 backlog sweep: queue unreadable",
			"component", "agent_send", "dir", q.Dir(), "err", err)
		return
	}
	now := time.Now()
	for _, b := range backlogs {
		switch {
		case !s.agentIsRegistered(b.Agent):
			s.reapBacklogForMissingAgent(b, now)
		case s.flightState(b.Agent) == FlightInFlight:
			s.reportStalledBacklog(b, now, "its turn is still in flight")
		default:
			if _, live := s.liveSender(b.Agent); !live {
				s.reportStalledBacklog(b, now, "it has no live process to deliver to")
				continue
			}
			slog.Info("🎯T418 backlog sweep: re-offering a held message",
				"component", "agent_send", "agent", b.Agent, "queued", b.Depth,
				"oldest_age", b.OldestAge(now).Round(time.Second).String())
			s.drainAgentSendQueue(b.Agent)
		}
	}
}

// agentIsRegistered reports whether the fleet still has a seat by this name.
// Read from the registry rather than from a live process: an agent that is
// stopped but registered can be rehydrated, and its backlog is still deliverable.
func (s *Server) agentIsRegistered(name string) bool {
	if s.registry == nil {
		// No registry to ask (hermetic servers, tests with only a send seam):
		// absence of evidence is not a removed seat, so keep the backlog.
		return true
	}
	for _, d := range s.registry.List() {
		if d.Name == name {
			return true
		}
	}
	return false
}

// reapBacklogForMissingAgent drops a queue whose addressee has left the fleet,
// after saying what was in it. The payload is named by size and age rather than
// quoted: the point is that someone learns a message was never delivered, and
// pasting an arbitrarily long payload into a health notice buries that.
func (s *Server) reapBacklogForMissingAgent(b sendq.Backlog, now time.Time) {
	slog.Error("🎯T418 backlog dropped: the agent it was addressed to is no longer registered",
		"component", "agent_send",
		"agent", b.Agent,
		"queued", b.Depth,
		"oldest_age", b.OldestAge(now).Round(time.Second).String())
	s.notifyFleetHealth(fmt.Sprintf(
		"UNDELIVERED: %d message(s) queued for %q were never delivered — that agent is no longer in the registry, "+
			"so there is nothing to deliver them to. The oldest had been waiting %s. "+
			"They are dropped now rather than held against a seat nobody will fill; re-send to a live agent if they still matter.",
		b.Depth, b.Agent, b.OldestAge(now).Round(time.Second)))
	if err := s.sendQueue().Clear(b.Agent); err != nil {
		slog.Error("🎯T418 backlog for a departed agent could not be cleared",
			"component", "agent_send", "agent", b.Agent, "err", err)
	}
}

// reportStalledBacklog names a queue that has stopped moving. It fires once the
// head has been waiting longer than StalledBacklogAfter and then at most once
// per sweep, because an alarm that fires for the normal state of the world is
// not loud (🎯T426): a queue thirty seconds behind a working agent is a queue
// doing its job.
//
// 🎯T527: when fleet intent says the agent should not be running (parked,
// blocked, reaped), the notice names that stand-down and does NOT prescribe
// interrupt=true — interrupting a deliberate park fights 🎯T414.
func (s *Server) reportStalledBacklog(b sendq.Backlog, now time.Time, why string) {
	age := b.OldestAge(now)
	if age < StalledBacklogAfter {
		return
	}

	// 🎯T527: deliver_start is the control that would revive a stopped seat to
	// drain this queue. If intent declines it, the backlog is held on purpose —
	// name the park, never prescribe interrupt (that fights 🎯T414).
	dec := s.AllowFleetControl(b.Agent, fleetintent.ControlDeliverStart)
	if !dec.Allow {
		reason := "stood down: " + fleetintent.Describe(dec.Blocking)
		slog.Warn("🎯T418 backlog stalled",
			"component", "agent_send",
			"agent", b.Agent,
			"queued", b.Depth,
			"oldest_age", age.Round(time.Second).String(),
			"reason", reason,
			"intent", dec.Reason)
		s.notifyFleetHealth(fmt.Sprintf(
			"Stalled backlog on %q: %d message(s) held by the daemon, the oldest waiting %s. "+
				"Agent is stood down (%s; %s) — intentional stand-down; messages stay on disk until "+
				"the park is lifted (jevons_fleet_intent name=%q state=working). "+
				"No recovery action: a deliberate park is not a stuck turn.",
			b.Agent, b.Depth, age.Round(time.Second),
			fleetintent.Describe(dec.Blocking), dec.Reason, b.Agent))
		return
	}

	slog.Warn("🎯T418 backlog stalled",
		"component", "agent_send",
		"agent", b.Agent,
		"queued", b.Depth,
		"oldest_age", age.Round(time.Second).String(),
		"reason", why)
	s.notifyFleetHealth(fmt.Sprintf(
		"Stalled backlog on %q: %d message(s) held by the daemon, the oldest waiting %s, because %s. "+
			"They are on disk and will be delivered when it next takes a turn. If that agent should not still be busy, "+
			"confirm from ITS transcript (terminal assistant message + turn_duration, file not growing) and then "+
			"jevons_agent_send with interrupt=true.",
		b.Agent, b.Depth, age.Round(time.Second), why))
}
