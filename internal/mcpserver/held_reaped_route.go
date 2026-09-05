// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// 🎯T582 — A BACKLOG ON A FINISHED SEAT IS NOT AN OUTAGE.
//
// 🎯T401 made a reaped name a reachable address: gate feedback sent to a seat
// that has already been deregistered is held rather than dropped, and the
// sweep says so instead of losing it. What it did not decide was who the
// holding is FOR. The sweep ran every thirty seconds, saw the same aged
// backlog, and told the overseer again — on 2026-08-29 one post-achieve gate
// note to jv-t577-no-checkpoint-reap produced twenty-six identical
// "[Fleet health] Held backlog on reaped agent" alerts over thirteen minutes,
// each one advising jevons_agent_start on a seat that had finished its work.
//
// The advice was worse than the repetition. Resurrecting a finished seat to
// drain feedback about its own achieved target spends a pane on a mission that
// is over, and the fleet's answer to "this worker is done" cannot be "start it
// again". The message has a live reader already: the parent that spawned the
// seat, which owns whatever follow-up the feedback implies.
//
// So a backlog held for a seat reaped by reap_achieve / reap_done is ROUTED,
// not re-announced:
//
//   - to the seat's PARENT, tagged with the reaped name, so the PO reads it as
//     feedback about a worker it owns rather than as a message from nowhere;
//   - or DROPPED with one eventlog record when it is gate feedback about the
//     very target the reap rode on — there is nothing left to act on, and the
//     honest record of that is a log line, not a queue nobody drains;
//   - and the overseer hears about a given reaped seat AT MOST ONCE, because
//     an alarm that repeats every sweep for the normal aftermath of a finish
//     is not loud, it is background (🎯T426).
//
// 🎯T401's contract is untouched: a genuinely new message to a reaped name is
// still accepted and held (that is the send path, not the sweep), and the
// sender still reads reaped-with-reason plus the recovery call. This file is
// only about what happens to a hold nobody came back for.

// heldReapedRouteAfter is how long a hold sits before the sweep routes it. It
// matches the reporting threshold the reaped notice already used: a queue a
// few seconds behind a just-reaped seat may still be drained by a restart the
// parent asked for (🎯T530's grace), and routing it early would take the
// message away from the seat that was about to come back for it.
const heldReapedRouteAfter = StalledBacklogAfter

// reapRoutable reports whether a reaped record is one this routing applies to:
// the finish-hygiene reaps (🎯T165 / 🎯T195), not every stamp that happens to
// resolve to Reaped. A seat parked or killed for other reasons keeps the
// 🎯T401 hold-and-report behaviour, because there the seat may genuinely be
// restarted to read its backlog.
func reapRoutable(rec fleetintent.Record) bool {
	by := strings.ToLower(rec.By)
	return strings.Contains(by, fleetlog.ReasonReapAchieve) ||
		strings.Contains(by, fleetlog.ReasonReapDone)
}

// reapedOnAchieve distinguishes the two finish reaps. Only an achieve reap can
// make feedback moot by itself: the target it named is closed, so gate notes
// about that work have no remaining actor. A reap_done seat finished its own
// report but its target may still be open, so its backlog goes to the parent.
func reapedOnAchieve(rec fleetintent.Record) bool {
	return strings.Contains(strings.ToLower(rec.By), fleetlog.ReasonReapAchieve)
}

// looksLikeGateFeedback recognises the message class that produced the
// incident: a gate/verification note addressed to a worker about the commit it
// just finished. It is deliberately narrow — anything it does not recognise is
// routed to the parent rather than dropped, because a wrong route costs a
// reader's attention and a wrong drop costs the message.
func looksLikeGateFeedback(text string) bool {
	t := strings.ToLower(text)
	for _, marker := range []string{
		"gate ", "false-green", "false green", "gate feedback",
		"exit=", "suspect", "master is red",
	} {
		if strings.Contains(t, marker) {
			return true
		}
	}
	return false
}

// parentOfReaped names the seat's parent. The registry row is gone by the time
// this runs — that is what reaped means — so the answer comes from the removal
// account, which recorded the lineage at the moment it dropped the row
// (🎯T435). An empty answer is not a guess: without a parent there is nobody
// to route to, and the message is dropped with its record rather than
// delivered to an invented address.
func (s *Server) parentOfReaped(name string) string {
	acct := s.RemovalAccount()
	if acct == nil {
		return ""
	}
	// Newest wins: a name reaped, restarted, and reaped again should route to
	// the parent of the most recent seat.
	var parent string
	for _, n := range acct.Recent(24 * time.Hour) {
		if n.Name == name && strings.TrimSpace(n.Parent) != "" {
			parent = strings.TrimSpace(n.Parent)
		}
	}
	return parent
}

// noticedReapedBacklog reports whether the overseer has already been told
// about this seat's held backlog, marking it told on the first call. One
// notice per reaped seat is the whole point of the target: the sweep runs on a
// timer, and a timer plus an unconditional notify is a repeating alarm.
func (s *Server) noticedReapedBacklog(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heldReapedNoticed == nil {
		s.heldReapedNoticed = map[string]bool{}
	}
	if s.heldReapedNoticed[name] {
		return true
	}
	s.heldReapedNoticed[name] = true
	return false
}

// forgetReapedBacklogNotice lets a name be announced again after a genuine new
// hold. Called when the seat comes back (a start clears the reaped stamp, so
// the sweep stops taking this path) — keeping the flag forever would silence a
// second, unrelated backlog under the same name.
func (s *Server) forgetReapedBacklogNotice(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.heldReapedNoticed, name)
}

// routeHeldReapedBacklog is the sweep's answer for a queue held against a seat
// reaped on finish. It drains the queue itself — to the parent, or to the
// eventlog — and tells the overseer once.
func (s *Server) routeHeldReapedBacklog(b sendq.Backlog, rec fleetintent.Record, now time.Time) {
	age := b.OldestAge(now)
	q := s.sendQueue()
	parent := s.parentOfReaped(b.Agent)
	achieve := reapedOnAchieve(rec)

	routed, dropped := 0, 0
	for _, id := range b.EntryIDs {
		e, claimed, err := q.ClaimFrontID(b.Agent, id)
		if err != nil {
			slog.Error("held backlog claim failed", "agent", b.Agent, "entry_id", id, "err", err)
			break
		}
		if !claimed {
			continue // A concurrent drain owns or already resolved this entry.
		}
		outcome := sendq.TerminalUndelivered

		switch {
		case achieve && looksLikeGateFeedback(e.Text):
			// The target this feedback is about is the one the reap rode on.
			// Nobody is going to act on it; the honest record is a log line.
			s.LogEvent("agent_send", "held_backlog_dropped", map[string]any{
				"target":     "T582",
				"agent":      b.Agent,
				"entry_id":   e.ID,
				"reason":     "gate feedback about an already-achieved target",
				"intent":     rec.Describe(),
				"bytes":      len(e.Text),
				"oldest_age": age.Round(time.Second).String(),
			})
			dropped++
		case parent != "":
			tagged := fmt.Sprintf(
				"[held backlog from %s — that seat was %s, so this is routed to you as its parent (🎯T582)]\n\n%s",
				b.Agent, rec.Describe(), e.Text)
			res, derr := s.deliverByName(parent, tagged, OriginAgent, false)
			if derr == nil {
				switch res.Status {
				case "sent", "interrupted_sent", "rehydrated_sent":
					// Retains the existing direct-send success predicate.
				case "queued", "interrupted_queued", StatusReapedHeld:
					if res.Queued == 0 || !q.Durable() {
						derr = fmt.Errorf("parent hold is not a confirmed durable successor: %s", res.Message)
					}
				default:
					derr = fmt.Errorf("parent delivery remains %s: %s", res.Status, res.Message)
				}
			}
			if derr != nil {
				slog.Warn("🎯T582 held backlog could not be routed to the parent",
					"component", "agent_send", "agent", b.Agent, "parent", parent, "err", derr)
				s.LogEvent("agent_send", "held_backlog_uncertain", map[string]any{
					"target":   "T582",
					"agent":    b.Agent,
					"entry_id": e.ID,
					"parent":   parent,
					"reason":   "parent delivery unverified; payload retained: " + derr.Error(),
					"bytes":    len(e.Text),
				})
				if err := q.Resolve(b.Agent, e, sendq.Unverified, "parent delivery outcome uncertain: "+derr.Error()); err != nil {
					slog.Error("held backlog route outcome not persisted", "agent", b.Agent, "entry_id", e.ID, "err", err)
				}
				s.notifyFleetHealth(fmt.Sprintf("Uncertain route of held message %s from %q to %q. The payload remains held; reconcile before retrying.", e.ID, b.Agent, parent))
				return
			}
			outcome = sendq.Confirmed
			routed++
		default:
			s.LogEvent("agent_send", "held_backlog_dropped", map[string]any{
				"target":   "T582",
				"agent":    b.Agent,
				"entry_id": e.ID,
				"reason":   "reaped seat has no recorded parent to route to",
				"intent":   rec.Describe(),
				"bytes":    len(e.Text),
			})
			dropped++
		}
		if err := q.Resolve(b.Agent, e, outcome, "recorded reaped-seat disposition"); err != nil {
			slog.Error("held backlog route not persisted; attempt retained", "agent", b.Agent, "entry_id", e.ID, "err", err)
			return
		}
	}
	if routed+dropped == 0 {
		return
	}

	slog.Info("🎯T582 held backlog on a finished seat routed",
		"component", "agent_send",
		"agent", b.Agent,
		"parent", parent,
		"routed", routed,
		"dropped", dropped,
		"oldest_age", age.Round(time.Second).String(),
		"intent", rec.Describe())

	if s.noticedReapedBacklog(b.Agent) {
		return
	}
	var b2 strings.Builder
	fmt.Fprintf(&b2, "Held backlog on finished agent %q (%s): %d message(s) waiting %s. ",
		b.Agent, rec.Describe(), b.Depth, age.Round(time.Second))
	switch {
	case routed > 0 && dropped > 0:
		fmt.Fprintf(&b2, "%d routed to its parent %q (tagged with the reaped name); %d dropped as gate feedback about work already achieved (one eventlog record each). ",
			routed, parent, dropped)
	case routed > 0:
		fmt.Fprintf(&b2, "Routed to its parent %q, tagged with the reaped name. ", parent)
	default:
		fmt.Fprintf(&b2, "Dropped with an eventlog record: it is feedback about work already finished, and there is no live seat it belongs to. ")
	}
	b2.WriteString("No action: the seat finished its mission and was deregistered — do NOT jevons_agent_start it to drain this. " +
		"This notice is sent once per reaped seat (🎯T582).")
	s.notifyFleetHealth(b2.String())
}
