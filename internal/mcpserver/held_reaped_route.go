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
// So a backlog held for a seat reaped by reap_achieve / reap_done is not
// re-announced every sweep:
//
//   - a message that is not gate feedback about an achieve reap, and that has
//     a recorded parent, goes to that PARENT, tagged with the reaped name, so
//     the PO reads it as feedback about a worker it owns;
//   - gate feedback about the target an achieve reap closed, and any hold
//     whose seat has no recorded parent, STAYS in the queue. The daemon does
//     not claim it and does not discard it. jevons_sendq_reconcile or an
//     overseer kill is the recorded exit. One backlog_held event names the
//     head entry id and its byte size, not the text;
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
		strings.Contains(by, fleetlog.ReasonReapDone) ||
		strings.Contains(by, fleetlog.ReasonReapStop) // 🎯T985 stop-to-reap
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
// just finished. It is deliberately narrow. Anything it does not recognise is
// routed to the recorded parent. A match on an achieve reap is left in the
// queue: the daemon does not deliver it and does not discard it.
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
// to route to. The message stays in the queue rather than being delivered
// to an invented address or discarded by the sweep.
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

// heldBacklogKey identifies THIS hold, not merely the seat it is held against.
// A name can be reaped, restarted, drained and reaped again; keying the notice
// on the name alone would silence the second, genuinely new backlog forever,
// because forgetReapedBacklogNotice only runs on a sweep that sees the name
// registered AND still holding. The head entry id is stable for the life of a
// hold — appending another message to the same queue does not change it — and
// differs the moment a new hold forms.
func heldBacklogKey(b sendq.Backlog) string {
	if len(b.EntryIDs) > 0 {
		return b.EntryIDs[0]
	}
	return b.Oldest.UTC().Format(time.RFC3339Nano)
}

// noticedReapedBacklog reports whether the overseer has already been told
// about this hold on this seat, marking it told on the first call. One notice
// per held backlog is the whole point of the target: the sweep runs on a
// timer, and a timer plus an unconditional notify is a repeating alarm.
func (s *Server) noticedReapedBacklog(name, hold string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heldReapedNoticed == nil {
		s.heldReapedNoticed = map[string]string{}
	}
	if s.heldReapedNoticed[name] == hold {
		return true
	}
	s.heldReapedNoticed[name] = hold
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
// reaped on finish. A non-gate hold with a recorded parent is delivered to
// that parent. Gate feedback about an achieve reap, and any hold with no
// recorded parent, stays in its current phase until jevons_sendq_reconcile
// records a drop or an overseer kill discards it. The overseer hears once.
func (s *Server) routeHeldReapedBacklog(b sendq.Backlog, rec fleetintent.Record, now time.Time) {
	age := b.OldestAge(now)
	q := s.sendQueue()
	parent := s.parentOfReaped(b.Agent)
	achieve := reapedOnAchieve(rec)

	rows, err := q.Snapshot(b.Agent)
	if err != nil {
		slog.Error("held backlog unreadable", "agent", b.Agent, "err", err)
		return
	}
	byID := make(map[string]sendq.Entry, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	routed, held := 0, 0
	var heldHead sendq.Entry
	for _, id := range b.EntryIDs {
		seen, ok := byID[id]
		if !ok {
			continue
		}
		// Do not claim. Claiming would mark the entry Attempting, and the
		// Resolve that followed would be a daemon disposition. The payload
		// stays in the phase it already has.
		if parent == "" || (achieve && looksLikeGateFeedback(seen.Text)) {
			if held == 0 {
				heldHead = seen
			}
			held++
			continue
		}
		// ClaimID, not ClaimFrontID. The kept head is still the first
		// claimable entry, and ClaimFrontID will not take the one behind it.
		e, claimed, err := q.ClaimID(b.Agent, id)
		if err != nil {
			slog.Error("held backlog claim failed", "agent", b.Agent, "entry_id", id, "err", err)
			break
		}
		if !claimed {
			continue
		}
		tagged := fmt.Sprintf(
			"[held backlog from %s — that seat was %s, so this is routed to you as its parent (🎯T582)]\n\n%s",
			b.Agent, rec.Describe(), e.Text)
		res, derr := s.deliverByName(parent, tagged, OriginAgent, false)
		// sent, interrupted_sent, and rehydrated_sent mean the parent
		// call returned. That is not a receipt. Keep the payload and do
		// not offer it again. A durable queue successor is still a hold
		// the parent accepted into its own queue.
		unconfirmedHandoff := false
		if derr == nil {
			switch res.Status {
			case "sent", "interrupted_sent", "rehydrated_sent":
				unconfirmedHandoff = true
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
			s.notifyFleetHealth(e.ID, fmt.Sprintf("Uncertain route of held message %s from %q to %q. The payload remains held; reconcile before retrying.", e.ID, b.Agent, parent))
			return
		}
		outcome := sendq.Confirmed
		detail := "recorded reaped-seat disposition"
		if unconfirmedHandoff {
			outcome = sendq.Unverified
			detail = "parent accepted the call without a receipt; payload retained"
		}
		routed++
		if err := q.Resolve(b.Agent, e, outcome, detail); err != nil {
			slog.Error("held backlog route not persisted; attempt retained", "agent", b.Agent, "entry_id", e.ID, "err", err)
			return
		}
	}
	if routed+held == 0 {
		return
	}
	hold := heldBacklogKey(b)
	if s.noticedReapedBacklog(b.Agent, hold) {
		return
	}
	if held > 0 {
		reason := "reaped seat has no recorded parent to route to; payload stays"
		if achieve && looksLikeGateFeedback(heldHead.Text) {
			reason = "gate feedback about an already-achieved target; payload stays"
		}
		s.LogEvent("agent_send", "backlog_held", map[string]any{
			"target":   "T582",
			"agent":    b.Agent,
			"entry_id": heldHead.ID,
			"reason":   reason,
			"bytes":    len(heldHead.Text),
			"queued":   held,
		})
	}
	slog.Info("🎯T582 held backlog on a finished seat",
		"component", "agent_send",
		"agent", b.Agent,
		"parent", parent,
		"routed", routed,
		"held", held,
		"oldest_age", age.Round(time.Second).String(),
		"intent", rec.Describe())

	var b2 strings.Builder
	fmt.Fprintf(&b2, "Held backlog on finished agent %q (%s): %d message(s) waiting %s. ",
		b.Agent, rec.Describe(), b.Depth, age.Round(time.Second))
	switch {
	case routed > 0 && held > 0:
		fmt.Fprintf(&b2, "%d routed to its parent %q (tagged with the reaped name); %d left in place (entry %s, %d bytes). The payload stays. Record a drop with jevons_sendq_reconcile, or an overseer kill discards it. The daemon will not discard it. ",
			routed, parent, held, heldHead.ID, len(heldHead.Text))
	case routed > 0:
		fmt.Fprintf(&b2, "Routed to its parent %q, tagged with the reaped name. ", parent)
	default:
		fmt.Fprintf(&b2, "The payload stays (entry %s, %d bytes). Record a drop with jevons_sendq_reconcile, or an overseer kill discards it. The daemon will not discard it. ",
			heldHead.ID, len(heldHead.Text))
	}
	b2.WriteString("No action: the seat finished its mission and was deregistered — do NOT jevons_agent_start it to drain this. " +
		"This notice is sent once per reaped seat (🎯T582).")
	s.notifyFleetHealth(hold, b2.String())
}
