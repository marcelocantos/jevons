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
	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/turnev"
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
// An unusable directory remains the queue's configured destination. Enqueue
// then fails visibly instead of accepting messages into a memory fallback.
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
	s.mu.Lock()
	s.agentSendQ = sendq.NewStore(dir)
	s.mu.Unlock()
	s.setBirthStoreDir(stateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("send queue unavailable: state directory unusable; enqueue will fail",
			"component", "agent_send", "dir", dir, "err", err)
		return
	}
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
	keys := make([]string, 0, len(backlogs))
	for _, b := range backlogs {
		keys = append(keys, heldBacklogKey(b))
	}
	s.notifyFleetHealth(strings.Join(keys, ","), fmt.Sprintf(
		"Recovered %d queued message(s) with outstanding delivery obligations: %s. "+
			"Pending entries are offered at an agent's next turn boundary. Unresolved attempts remain held; "+
			"their outcome is uncertain and they must be reconciled before any retry.",
		total, strings.Join(lines, "; ")))
}

// reconcileHeldFromTranscript settles held attempts the receiver's transcript
// now shows it received, and reports how many it settled.
//
// An attempt goes Uncertain when the confirm window closes with no record of
// the payload. For a Claude seat that is mid-turn, the window closing proves
// little: Claude Code holds a submitted message in its own input queue until
// the running turn ends, and the transcript gains it only then — routinely
// later than the window. On 2026-09-21 claudia-po held seven such attempts,
// the oldest 28 hours old, each naming a report_id that was sitting in its
// transcript. Only an operator could clear them, none had, and 244 messages
// were queued around them.
//
// This is not the automatic discard 🎯T623 forbids. Nothing is dropped on a
// guess: an entry is settled only on the positive evidence the send itself
// would have accepted — a user message, a queue record, or a queued payload
// entering a turn. An entry the transcript does not show stays held.
//
// The scan starts at the top of the transcript because the entry does not
// record where the transcript stood when it was attempted. The cost is that
// an earlier delivery of identical text settles a later one; the receiver
// then holds that text once rather than twice.
func (s *Server) reconcileHeldFromTranscript(q *sendq.Store, name string) int {
	if s == nil || s.registry == nil {
		return 0
	}
	def := s.registry.Def(name)
	proc := s.registry.Get(name)
	if def == nil || proc == nil {
		return 0
	}
	if spool.ResumeFromSpool(string(def.Provider), def.OMP) {
		if path, err := spool.EnsureView(spool.Dir(), def.Name); err == nil && path != "" {
			return s.settleHeldAgainst(q, name, path)
		}
	}
	if !providerKeepsClaudeTranscript(def.Provider) {
		return 0
	}
	return s.settleHeldAgainst(q, name, proc.JSONLPath())
}

// settleHeldAgainst is reconcileHeldFromTranscript once the receiver's
// transcript is known.
func (s *Server) settleHeldAgainst(q *sendq.Store, name, path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	// Re-reading a multi-megabyte transcript for every held entry on every
	// sweep would be the cost; only a transcript that grew can say more.
	s.mu.Lock()
	if s.heldScannedAt == nil {
		s.heldScannedAt = make(map[string]int64)
	}
	unchanged := s.heldScannedAt[name] == info.Size()
	s.heldScannedAt[name] = info.Size()
	s.mu.Unlock()
	if unchanged {
		return 0
	}
	entries, err := q.Snapshot(name)
	if err != nil {
		return 0
	}
	settled := 0
	for _, e := range entries {
		if e.State != sendq.Uncertain {
			continue
		}
		ev, ok := fateEvidence(path, 0, true, turnev.Needle(e.Text))
		if !ok {
			continue
		}
		if err := q.Resolve(name, e, sendq.Confirmed, ev.Detail); err != nil {
			slog.Warn("held attempt is in the transcript but could not be settled",
				"component", "agent_send", "agent", name, "entry_id", e.ID, "err", err)
			continue
		}
		settled++
		slog.Info("held attempt settled from the receiver's transcript",
			"component", "agent_send", "agent", name, "entry_id", e.ID,
			"attempt_id", e.AttemptID, "enqueued_at", e.EnqueuedAt, "evidence", ev.Detail)
	}
	return settled
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
	now := s.sweepClock()
	for _, b := range backlogs {
		switch {
		case b.Uncertain > 0:
			// An attempt nobody could confirm in the window may have been
			// confirmed by the receiver since. Look before pinning it.
			if settled := s.reconcileHeldFromTranscript(q, b.Agent); settled > 0 {
				b.Uncertain -= settled
				b.Depth -= settled
				if b.Uncertain <= 0 {
					if b.Depth > 0 && s.flightState(b.Agent) != FlightInFlight {
						if _, live := s.liveSender(b.Agent); live {
							s.drainAgentSendQueue(b.Agent)
						}
					}
					break
				}
			}
			// Never route, discard, or replay an attempt left by this daemon or
			// its predecessor (🎯T623). PINNED is a live-seat word (🎯T599):
			// a reaped or departed name must not keep raising it (🎯T686).
			pin, ok := s.sendqPinFor(b.Agent)
			if !ok || pin.AttemptID == "" {
				break
			}
			if rec, reaped := LookupReapedRecord(s.fleetIntent(), b.Agent); reaped {
				s.noticeUncertainAttempt(b.Agent, pin, FormatReapedUncertainHoldLine(b.Agent, pin, rec))
				break
			}
			if s.registry != nil && !s.agentIsRegistered(b.Agent) {
				break
			}
			s.noticeUncertainAttempt(b.Agent, pin, FormatSendqPinLine(b.Agent, pin))
			// 🎯T766.5: the held entry is never touched here, but the ones
			// behind it are ordinary pending messages. Before flow-past this
			// branch was a freeze by design, so it never re-offered anything —
			// and after a restart it was the only branch a queue with one
			// held entry could reach, so the queue waited for a turn boundary
			// the bounce had already consumed. jevons-po sat at 53 pending
			// behind 4 held for as long as it stayed idle.
			if b.Depth > b.Uncertain && s.flightState(b.Agent) != FlightInFlight {
				if _, live := s.liveSender(b.Agent); live {
					slog.Info("🎯T766.5 backlog sweep: re-offering pending messages behind held entries",
						"component", "agent_send", "agent", b.Agent, "queued", b.Depth, "held", b.Uncertain)
					s.drainAgentSendQueue(b.Agent)
				}
			}
		case !s.agentIsRegistered(b.Agent):
			// 🎯T401: a reaped seat is recoverable — gate feedback stays held
			// until jevons_agent_start (or intent lift + start) recreates it.
			// A never-registered / aged-out name still drops (T418 clause 5).
			if rec, ok := LookupReapedRecord(s.fleetIntent(), b.Agent); ok {
				s.reportHeldReapedBacklog(b, rec, now)
				continue
			}
			s.reapBacklogForMissingAgent(b, now)
		case s.flightState(b.Agent) == FlightInFlight:
			// A name that is registered again is a live seat: a later hold
			// under it deserves its own notice (🎯T582).
			s.forgetReapedBacklogNotice(b.Agent)
			s.reportStalledBacklog(b, now, "its turn is still in flight")
		default:
			s.forgetReapedBacklogNotice(b.Agent)
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

// reportHeldReapedBacklog names a queue held for a finished-and-reaped agent
// (🎯T401). It does not drop the messages — that would erase gate feedback —
// and it does not prescribe interrupt (that fights 🎯T414).
func (s *Server) reportHeldReapedBacklog(b sendq.Backlog, rec fleetintent.Record, now time.Time) {
	age := b.OldestAge(now)
	if age < StalledBacklogAfter {
		return
	}
	// 🎯T530: a parent-kill drain restart is mid-flight — do not regenerate
	// reaped_held solely from that kill while RemintGraceWindow holds.
	if s.suppressHeldReapedDuringDrainRestart(b.Agent, now) {
		slog.Info("🎯T530 suppressing reaped_held during drain restart grace",
			"component", "agent_send",
			"agent", b.Agent,
			"queued", b.Depth,
			"oldest_age", age.Round(time.Second).String())
		return
	}
	// 🎯T582: a seat reaped on finish is not coming back for this. Route the
	// hold to its parent (or drop gate feedback about the achieved target with
	// one eventlog record) and tell the overseer once, rather than advising
	// jevons_agent_start on a finished seat every sweep.
	if age >= heldReapedRouteAfter && reapRoutable(rec) {
		s.routeHeldReapedBacklog(b, rec, now)
		return
	}
	// 🎯T706: this branch is the sweep's, and the sweep is a timer. 🎯T582 put
	// the once-per-seat seam on the ROUTED path only, so every reap the routing
	// does not claim — an explicit kill, a stop_engagement, a dead_seat sweep —
	// fell through to an unconditional notify and alarmed every thirty seconds
	// for as long as the hold sat there. jv-t679.1-evidence-seam did exactly
	// that for three hours after a product:kill.
	//
	// The hold here is genuinely recoverable (unlike 🎯T582's finished seat), so
	// the recovery call stays in the notice. It is said ONCE per hold: repeating
	// it does not make it more actionable, and an alarm that fires every sweep
	// for a stable state is background, not signal (🎯T426).
	hold := heldBacklogKey(b)
	if s.noticedReapedBacklog(b.Agent, hold) {
		slog.Debug("🎯T706 held backlog on reaped agent already announced",
			"component", "agent_send",
			"agent", b.Agent,
			"queued", b.Depth,
			"oldest_age", age.Round(time.Second).String(),
			"intent", rec.Describe())
		return
	}
	slog.Warn("🎯T401 backlog held for reaped agent",
		"component", "agent_send",
		"agent", b.Agent,
		"queued", b.Depth,
		"oldest_age", age.Round(time.Second).String(),
		"intent", rec.Describe())
	// The hold's head entry id is in the line because the notice is now said
	// once: two successive holds on the same name would otherwise render
	// byte-identical, and the overseer's own replay digest collapses an exact
	// repeat — so the second, genuinely new backlog would be suppressed by the
	// delivery path even though the seam released it. Naming the hold makes the
	// second notice new information rather than an echo.
	s.notifyFleetHealth(hold, fmt.Sprintf(
		"Held backlog on reaped agent %q (hold %s): %d message(s) waiting %s (%s). "+
			"Recover with jevons_agent_start name=%q … — queued gate feedback drains on start. "+
			"Do not interrupt; the seat is finished-and-reaped, not stuck mid-turn. "+
			"This notice is sent once per held backlog (🎯T706); the hold stays on disk until the seat comes back or the queue is resolved.",
		b.Agent, hold, b.Depth, age.Round(time.Second), rec.Describe(), b.Agent))
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
	q := s.sendQueue()
	for _, id := range b.EntryIDs {
		e, claimed, err := q.ClaimFrontID(b.Agent, id)
		if err != nil {
			slog.Error("departed-agent backlog claim failed", "agent", b.Agent, "entry_id", id, "err", err)
			return
		}
		if !claimed {
			continue // A concurrent drain owns or already resolved this entry.
		}
		s.LogEvent("agent_send", "backlog_undelivered", map[string]any{
			"agent": b.Agent, "entry_id": e.ID, "attempt_id": e.AttemptID,
			"bytes": len(e.Text), "reason": "addressee no longer registered",
		})
		s.notifyFleetHealth(e.ID, fmt.Sprintf("UNDELIVERED: queued message %s (%d bytes) for %q has no registered addressee. "+
			"It waited %s and is being discarded with a terminal record; re-send to a live agent if still needed.",
			e.ID, len(e.Text), b.Agent, e.Age(now).Round(time.Second)))
		if err := q.Resolve(b.Agent, e, sendq.TerminalUndelivered, "addressee no longer registered; sender notified"); err != nil {
			slog.Error("backlog terminal outcome not persisted; attempt remains held", "agent", b.Agent, "entry_id", e.ID, "err", err)
			return
		}
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
		s.notifyFleetHealth(heldBacklogKey(b), fmt.Sprintf(
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
	s.notifyFleetHealth(heldBacklogKey(b), fmt.Sprintf(
		"Stalled backlog on %q: %d message(s) held by the daemon, the oldest waiting %s, because %s. "+
			"They are on disk and will be delivered when it next takes a turn. If that agent should not still be busy, "+
			"confirm from ITS transcript (terminal assistant message + turn_duration, file not growing) and then "+
			"jevons_agent_send with interrupt=true.",
		b.Agent, b.Depth, age.Round(time.Second), why))
}

// sweepClock is the sweep's now. Injectable so a ten-minute run of the sweep
// is a test that costs no wall time (🎯T582); the daemon leaves it nil.
func (s *Server) sweepClock() time.Time {
	s.mu.Lock()
	fn := s.sweepNow
	s.mu.Unlock()
	if fn == nil {
		return time.Now()
	}
	return fn()
}

// SetSweepClock injects the backlog sweep's clock. Test-only seam: the daemon
// never calls it, and nothing else in the server reads it.
func (s *Server) SetSweepClock(fn func() time.Time) {
	s.mu.Lock()
	s.sweepNow = fn
	s.mu.Unlock()
}

// ---- folded from t599_pinned_sendq.go (🎯T766: a fix expressed inside the thing it fixes) ----
// 🎯T599 — an undeliverable sendq message never makes a seat unkillable, and
// a pinned seat says so instead of looking busy.
//
// 2026-08-31: jv-t592-chatlog-turns was stranded on a Codex seat with a
// read-only sandbox. Its parent's kills were refused by the 🎯T530 guard
// ("daemon sendq still holds 1 message(s)"), and the one documented drain — a
// start — would have delivered exactly the message the overseer had ruled
// must not be delivered. The overseer's kill went through; that is the
// behaviour pinned here. Meanwhile agent_list showed an ordinary running
// seat: nothing said it could not be moved.
//
// Three parts:
//  1. An overseer kill always succeeds: the T530 hold yields to overseer
//     authority and the held messages are discarded, with one eventlog
//     record naming the count and the reason.
//  2. A parent kill still respects the guard, but the refusal names the
//     drain path and the explicit override instead of leaving the caller
//     with no way forward.
//  3. A seat whose sendq holds a message that cannot be delivered — the
//     provider cannot accept it, or delivery has failed
//     SendqPinFailureThreshold times — is reported by jevons_agent_list and
//     fleet health as PINNED with the blocking message named.

// SendqPinFailureThreshold is how many failed deliveries of the same queue
// entry make the seat pinned. One failure is weather (a pane mid-rotation);
// the same message failing repeatedly is a seat that cannot be moved by the
// documented drain.
const SendqPinFailureThreshold = 3

// SendqPin names the message that is blocking a seat: which entry, why, and
// how many delivery attempts have failed.
type SendqPin struct {
	EntryID   string
	Reason    string
	Fails     int
	At        time.Time
	State     sendq.DeliveryState
	AttemptID string
}

// FormatSendqPinLine is the operator-facing account of a pinned seat, shared
// by agent_list and the fleet-health notice so the two never drift.
func FormatSendqPinLine(name string, pin SendqPin) string {
	if pin.State != sendq.Pending {
		return fmt.Sprintf("PINNED %s: sendq message %s has an unresolved %s attempt %s (%s). "+
			"The payload remains held. A start will not retry it; messages behind it keep delivering past it (🎯T766.5). "+
			"Reconcile it with jevons_sendq_reconcile name=%[1]q (call it with only name= to see the queue, then "+
			"action=confirmed|requeue|drop entry_id=%[2]q attempt_id=%[4]q actor=… evidence=…) — 🎯T726. "+
			"Never edit ~/.jevons/sendq/*.json while the daemon is running. "+
			"An explicit overseer kill discards the held obligation without claiming non-delivery.",
			name, pin.EntryID, pin.State, pin.AttemptID, pin.Reason)
	}
	fails := ""
	if pin.Fails > 0 {
		fails = fmt.Sprintf("; %d failed deliveries", pin.Fails)
	}
	return fmt.Sprintf(
		"PINNED %s: sendq message %s cannot be delivered (%s%s). "+
			"A start would deliver it; an overseer kill discards it (🎯T599).",
		name, pin.EntryID, pin.Reason, fails)
}

// sendqPinFor answers whether a seat is pinned and by what.
func (s *Server) sendqPinFor(name string) (SendqPin, bool) {
	if s == nil || strings.TrimSpace(name) == "" {
		return SendqPin{}, false
	}
	// Read durable attempts, including those left by a previous daemon. An
	// in-memory pin alone disappears at exactly the restart that matters.
	if e, blocked, err := s.sendQueue().BlockedHead(name); err == nil && blocked {
		return SendqPin{EntryID: e.ID, AttemptID: e.AttemptID, State: e.State,
			Reason: e.Detail, At: e.EnqueuedAt}, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pin, ok := s.sendqPin[name]
	return pin, ok
}

// clearSendqPin forgets pin state for a seat: a delivered message, a drained
// queue, or a removed seat is no longer blocking anything.
func (s *Server) clearSendqPin(name string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.mu.Lock()
	delete(s.sendqPin, name)
	delete(s.sendqPinFails, name)
	delete(s.sendqAttemptNoticed, name)
	s.mu.Unlock()
}

// MarkSendqPinned pins a seat directly: the caller knows the head message
// cannot be delivered at all (the provider cannot accept it). Notifies fleet
// health once per pin.
func (s *Server) MarkSendqPinned(name string, e sendq.Entry, reason string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	pin := SendqPin{EntryID: e.ID, Reason: strings.TrimSpace(reason), At: time.Now()}
	s.mu.Lock()
	if s.sendqPinFails != nil {
		pin.Fails = s.sendqPinFails[name].fails
	}
	already := false
	if s.sendqPin == nil {
		s.sendqPin = map[string]SendqPin{}
	} else {
		_, already = s.sendqPin[name]
	}
	s.sendqPin[name] = pin
	s.mu.Unlock()
	if !already {
		s.notifyFleetHealth(pin.EntryID, FormatSendqPinLine(name, pin))
	}
}

// sendqEntryFails tracks repeated delivery failure of one queue entry.
type sendqEntryFails struct {
	entryID string
	fails   int
}

// noteSendqDeliveryFailure counts a failed delivery of a still-held entry
// and pins the seat when the same entry has failed SendqPinFailureThreshold
// times. A different entry resets the count: the queue moved, so the seat is
// not stuck on one message.
func (s *Server) noteSendqDeliveryFailure(name string, e sendq.Entry, reason string) {
	if s == nil || strings.TrimSpace(name) == "" || e.ID == "" {
		return
	}
	s.mu.Lock()
	if s.sendqPinFails == nil {
		s.sendqPinFails = map[string]sendqEntryFails{}
	}
	rec := s.sendqPinFails[name]
	if rec.entryID != e.ID {
		rec = sendqEntryFails{entryID: e.ID}
	}
	rec.fails++
	s.sendqPinFails[name] = rec
	s.mu.Unlock()
	if rec.fails >= SendqPinFailureThreshold {
		s.MarkSendqPinned(name, e, fmt.Sprintf("delivery failed: %s", strings.TrimSpace(reason)))
	}
}

// discardHeldSendqForOverseerKill drops a seat's whole held queue because the
// overseer is killing it — the T530 hold yields to overseer authority. One
// eventlog record names the count and the reason; the alternative was a seat
// that could not be moved off a broken provider because the only drain would
// deliver a message the overseer had ruled must not be delivered.
func (s *Server) discardHeldSendqForOverseerKill(name, actor string) (int, error) {
	entries, err := s.sendQueue().Discard(name)
	if err != nil {
		return 0, err
	}
	count := len(entries)
	ids := make([]string, 0, count)
	states := make([]sendq.DeliveryState, 0, count)
	for _, e := range entries {
		ids = append(ids, e.ID)
		states = append(states, e.State)
	}
	s.clearSendqPin(name)
	s.logLifecycle(compAgentLifecycle, "kill_discard_sendq", "ok", map[string]any{
		"name":            name,
		"actor":           actor,
		"discarded":       count,
		"entry_ids":       ids,
		"delivery_states": states,
		"reason": "overseer kill overrides the 🎯T530 hold: held messages are " +
			"explicitly discarded; unresolved attempts may already have reached the receiver (🎯T599)",
	})
	return count, nil
}

// ---- folded from t686_reaped_pinned.go (🎯T766: a fix expressed inside the thing it fixes) ----
// 🎯T686 — a reaped seat must not keep raising a PINNED fleet-health alert
// the owner cannot act on.
//
// 2026-09-20: cl-t81-grok-billing took a Cursor ACP prompt in flight, sendq
// 3a62e454aeb3 went uncertain (broker-wrapped "prompt already in flight"),
// and the seat was reaped at 00:51. SweepSendBacklogs runs every ~30s and
// treated Uncertain before the 🎯T401 reaped-address path, so fleet health
// kept composing PINNED for a name that was no longer registered.
// jevons_agent_kill is already a no-op on a reaped name and is not the
// remedy: 🎯T623 forbids discarding an uncertain attempt automatically, and
// 🎯T401 keeps the hold so the closed address stays recoverable.
//
// PINNED (🎯T599) is a live-seat word. After reap the same hold is a closed
// address: say so once, or stay silent — never a bare PINNED.

// FormatReapedUncertainHoldLine is the operator-facing account of an
// unresolved sendq attempt whose seat has already left the registry.
func FormatReapedUncertainHoldLine(name string, pin SendqPin, rec fleetintent.Record) string {
	closed := "finished-and-reaped"
	if d := rec.Describe(); d != "" {
		closed = d
	}
	return fmt.Sprintf(
		"reaped-with-reason %s: sendq message %s has an unresolved %s attempt %s (%s). "+
			"The seat is %s — a recoverable closed address, not a live PINNED seat. "+
			"The payload remains held; a start will not retry it. jevons_agent_kill is a no-op here (🎯T401/🎯T686). "+
			"Resolve the attempt with jevons_sendq_reconcile name=%[1]q entry_id=%[2]q attempt_id=%[4]q (🎯T726).",
		name, pin.EntryID, pin.State, pin.AttemptID, pin.Reason, closed)
}

// noticeUncertainAttempt delivers one fleet-health line per unresolved
// attempt id. The sweep is a timer; without the guard the same hold
// becomes a repeating alarm (🎯T582 / 🎯T686).
func (s *Server) noticeUncertainAttempt(name string, pin SendqPin, line string) {
	if s == nil || strings.TrimSpace(name) == "" || line == "" {
		return
	}
	s.mu.Lock()
	if s.sendqAttemptNoticed == nil {
		s.sendqAttemptNoticed = map[string]string{}
	}
	noticed := s.sendqAttemptNoticed[name] == pin.AttemptID
	s.sendqAttemptNoticed[name] = pin.AttemptID
	s.mu.Unlock()
	if !noticed {
		s.notifyFleetHealth(pin.AttemptID, line)
	}
}
