// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/converge"
)

// Owner-interaction convergence, server half (🎯T355).
//
// The cockpit reconciler (🎯T204) keeps the overseer *process* usable. This
// keeps the owner's *interaction* healthy, which is a different desired state
// and fails in ways a live process cannot see: a send that never reached the
// journal, a spinner over an idle server, a turn that evaporated in a bounce.
//
// The shape is the one 🎯T171 got right for daemon restart, generalized:
// observe a level, classify it, take a step, and let only the next
// observation decide satisfaction. Restart is one outage class among ACP
// stall, owner-turn stall, delivery failure and UX degrade — it is not the
// model. The pure classifier and the standing set live in internal/converge
// (owner.go); everything here is observation and actuation.

// ownerHealthState is the server's running level truth about owner
// interaction. Guarded by Server.ownerMu, which is never held across a call
// back into the server (the actuators broadcast and enqueue).
type ownerHealthState struct {
	set    *converge.OwnerSet
	bounds converge.OwnerBounds

	// Newest owner submission. echo is the exact journal line we expect to
	// see land, which is how send-landed is observed rather than assumed.
	sendSeq       int
	sendID        string
	sendText      string
	sendEcho      string
	sendAt        time.Time
	sendJournaled bool
	sendDelivered bool
	// requeuedID / requeuedAt dedupe re-injection: send-landed and
	// reply-or-residual can stand open at once over the same lost turn, and
	// the owner must be asked their own question at most once per round.
	requeuedID string
	requeuedAt time.Time

	// chromeWorking is the working level the clients were last told to
	// show; mismatchSince is when it started disagreeing with level truth.
	chromeWorking bool
	mismatchSince time.Time

	ownerTurnAt   time.Time
	replySealedAt time.Time
	residual      string
	residualAt    time.Time

	uiHeartbeatAt   time.Time
	composerBlocked bool
	composerReason  string

	// alerted records the dimensions whose gap reached the owner-visible
	// alert rung. The client raises a sticky banner on that alert (🎯T361),
	// so recovery has to be published too — otherwise the banner outlives
	// the gap and the owner is left distrusting a healthy chat.
	alerted map[converge.OwnerDimension]bool
}

// ownerHealthLocked returns the state with its lazy fields initialised.
// Caller holds ownerMu.
func (s *Server) ownerHealthLocked() *ownerHealthState {
	h := &s.ownerHealth
	if h.set == nil {
		h.set = converge.NewOwnerSet()
	}
	if h.bounds == (converge.OwnerBounds{}) {
		h.bounds = converge.DefaultOwnerBounds()
	}
	return h
}

// SetOwnerHealthBounds overrides the shipped tolerances (hermetic tests).
func (s *Server) SetOwnerHealthBounds(b converge.OwnerBounds) {
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	s.ownerHealthLocked().bounds = b
}

// hasChatLog reports whether a durable owner journal is configured.
func (s *Server) hasChatLog() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chatLog != nil
}

// NoteOwnerSend records an accepted owner submission: a new turn is owed a
// reply, the client has just lit its own working chrome, and neither
// durability nor delivery has been observed yet.
func (s *Server) NoteOwnerSend(text, echo string) {
	now := time.Now()
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	h := s.ownerHealthLocked()
	h.sendSeq++
	h.sendID = fmt.Sprintf("send-%d", h.sendSeq)
	h.sendText = text
	h.sendEcho = echo
	h.sendAt = now
	// With no durable log configured there is nothing to observe landing,
	// so durability is not a gap the owner can suffer on this deployment.
	h.sendJournaled = !s.hasChatLog()
	h.sendDelivered = false
	h.ownerTurnAt = now
	h.replySealedAt = time.Time{}
	h.residual = ""
	h.residualAt = time.Time{}
	// The client paints working chrome on its own send edge (🎯T272).
	h.chromeWorking = true
	h.mismatchSince = time.Time{}
}

// noteChatJournaled is called with every line that made it into the durable
// chatlog. The owner's own echo landing there is the durability half of
// send-landed — observed, not assumed.
func (s *Server) noteChatJournaled(line string) {
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	h := s.ownerHealthLocked()
	if h.sendEcho != "" && line == h.sendEcho {
		h.sendJournaled = true
	}
}

// NoteOwnerDelivered records the server ack half of send-landed: the owner's
// prompt actually left the queue into the overseer's session.
func (s *Server) NoteOwnerDelivered() {
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	s.ownerHealthLocked().sendDelivered = true
}

// NoteOwnerReplySealed records a sealed assistant reply for the open owner
// turn — the satisfying observation for reply-or-residual.
func (s *Server) NoteOwnerReplySealed() {
	now := time.Now()
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	h := s.ownerHealthLocked()
	h.replySealedAt = now
	// The reply is visible; the client settles its chrome on the seal.
	h.chromeWorking = false
	h.mismatchSince = time.Time{}
}

// NoteOwnerResidual records a named reason no reply is owed (cancelled by the
// owner, delivery failed, overseer restarted). A named residual satisfies
// reply-or-residual; silence never does.
func (s *Server) NoteOwnerResidual(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	now := time.Now()
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	h := s.ownerHealthLocked()
	h.residual = name
	h.residualAt = now
}

// NoteOwnerUIHeartbeat records a client heartbeat (the chat transport ping).
// A browser whose main thread has stopped also stops pinging, which is how
// the UX-degrade class is observed server-side.
func (s *Server) NoteOwnerUIHeartbeat() {
	now := time.Now()
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	s.ownerHealthLocked().uiHeartbeatAt = now
}

// NoteOwnerComposerBlocked records the client's own report that the owner
// cannot submit a turn (🎯T361). A ticking heartbeat says the page is alive;
// it says nothing about whether the composer will take a turn, so without
// this the UX-degrade class could only ever be observed as a frozen tab.
func (s *Server) NoteOwnerComposerBlocked(blocked bool, reason string) {
	reason = strings.TrimSpace(reason)
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	h := s.ownerHealthLocked()
	if h.composerBlocked != blocked {
		slog.Info("owner health: composer level reported",
			"blocked", blocked, "reason", reason)
	}
	h.composerBlocked = blocked
	if blocked {
		h.composerReason = reason
	} else {
		h.composerReason = ""
	}
	// A client that can report is a client that is ticking.
	h.uiHeartbeatAt = time.Now()
}

// noteChromePublished records the working level the clients were just told.
func (s *Server) noteChromePublished(working bool) {
	s.ownerMu.Lock()
	defer s.ownerMu.Unlock()
	h := s.ownerHealthLocked()
	if h.chromeWorking != working {
		h.chromeWorking = working
		h.mismatchSince = time.Time{}
	}
}

// publishedWorkingLevel samples level truth and records it as the chrome the
// clients were told to paint. Use it wherever a frame carries the working
// level to a client; use overseerWorkingLevel when only reading truth.
func (s *Server) publishedWorkingLevel() bool {
	working := s.overseerWorkingLevel()
	s.noteChromePublished(working)
	return working
}

// ObserveOwnerInteraction samples the owner-facing plant. It also advances
// the chrome-mismatch edge, because the level observer is the only place that
// sees both sides of that comparison.
func (s *Server) ObserveOwnerInteraction(now time.Time) converge.OwnerObservation {
	turnInFlight := s.overseerWorkingLevel()

	s.mu.RLock()
	clients := len(s.chatListeners)
	queueDepth := len(s.notifyQueue)
	lastProgress := s.overseerLastProgress
	proc := s.proc
	s.mu.RUnlock()

	o := converge.OwnerObservation{
		ClientsConnected: clients,
		TurnInFlight:     turnInFlight,
		QueueDepth:       queueDepth,
	}
	if proc != nil && proc.Alive() {
		o.PromptInFlight = proc.PromptInFlight()
	}
	if !lastProgress.IsZero() {
		o.SinceACPProgress = now.Sub(lastProgress)
	}

	s.ownerMu.Lock()
	h := s.ownerHealthLocked()
	o.SendID = h.sendID
	o.SendAt = h.sendAt
	o.SendJournaled = h.sendJournaled
	o.SendDelivered = h.sendDelivered
	o.ChromeWorking = h.chromeWorking
	o.OwnerTurnAt = h.ownerTurnAt
	o.ReplySealedAt = h.replySealedAt
	o.Residual = h.residual
	o.ResidualAt = h.residualAt
	o.ComposerBlocked = h.composerBlocked
	if !h.uiHeartbeatAt.IsZero() {
		o.SinceUIHeartbeat = now.Sub(h.uiHeartbeatAt)
	}
	// Maintain the mismatch edge from the one shared predicate.
	if converge.OwnerChromeMismatch(o) {
		if h.mismatchSince.IsZero() {
			h.mismatchSince = now
		}
	} else {
		h.mismatchSince = time.Time{}
	}
	o.ChromeMismatchSince = h.mismatchSince
	s.ownerMu.Unlock()

	return o
}

// ReconcileOwnerHealth runs one observe → classify → step pass. It is called
// on the cockpit tick; steps never close a gap, so a plant that stays broken
// stays in the set and escalates.
func (s *Server) ReconcileOwnerHealth(now time.Time) []converge.OwnerOutcome {
	obs := s.ObserveOwnerInteraction(now)

	s.ownerMu.Lock()
	h := s.ownerHealthLocked()
	set, bounds := h.set, h.bounds
	s.ownerMu.Unlock()

	outcomes := set.ReconcileAll(converge.ClassifyOwnerInteraction(obs, now, bounds), now)
	for _, out := range outcomes {
		switch out.Resolution {
		case converge.ResolutionOpened:
			slog.Warn("owner health: gap opened",
				"dimension", out.Dimension, "kind", out.Gap.Kind, "reason", out.Reason,
				"episode", out.Gap.Episode)
			s.LogEvent("owner_health", "gap_opened", map[string]any{
				"dimension": string(out.Dimension),
				"kind":      string(out.Gap.Kind),
				"reason":    out.Reason,
				"episode":   out.Gap.Episode,
			})
		case converge.ResolutionSatisfied:
			slog.Info("owner health: gap satisfied",
				"dimension", out.Dimension, "reason", out.Reason,
				"steps", out.Gap.StepCount(), "dwell", out.Gap.Age(now).String())
			s.LogEvent("owner_health", "gap_satisfied", map[string]any{
				"dimension": string(out.Dimension),
				"reason":    out.Reason,
				"steps":     out.Gap.StepCount(),
				"dwell_ms":  out.Gap.Age(now).Milliseconds(),
			})
			s.publishOwnerRecovered(out.Dimension, out.Reason)
		}
	}

	for _, rec := range set.Drive(now, converge.OwnerDue(bounds), ownerActuator{s}) {
		slog.Info("owner health: step",
			"step", rec.Kind, "key", rec.Key, "delivered", rec.Delivered, "err", rec.Err)
		s.LogEvent("owner_health", "step", map[string]any{
			"step":      string(rec.Kind),
			"key":       string(rec.Key),
			"delivered": rec.Delivered,
			"err":       rec.Err,
		})
	}
	return outcomes
}

// publishOwnerRecovered closes the loop on an escalation the owner can see.
// Only a dimension that actually reached the alert rung is announced — a gap
// that recovered before the owner was ever told does not deserve a
// "recovered" the owner cannot place.
//
// The frame is a status with no state and a ux marker: status survives the
// soft-reconnect filter (🎯T143), and carrying no state keeps it from
// settling working chrome mid-turn on its way to clearing the banner
// (🎯T260). It is live-only, not journaled: chrome truth, not a turn.
func (s *Server) publishOwnerRecovered(dim converge.OwnerDimension, reason string) {
	s.ownerMu.Lock()
	h := s.ownerHealthLocked()
	alerted := h.alerted[dim]
	if alerted {
		delete(h.alerted, dim)
	}
	s.ownerMu.Unlock()
	if !alerted {
		return
	}

	text := fmt.Sprintf("owner interaction recovered: %s (%s)", dim, reason)
	payload, err := json.Marshal(map[string]any{
		"type": "status",
		"ux":   "recovered",
		"text": text,
	})
	if err != nil {
		slog.Error("owner health: marshal recovery frame", "err", err)
		return
	}
	slog.Info("owner health: alerted gap recovered", "dimension", dim, "reason", reason)
	s.broadcastChatLive(string(payload))
}

// OwnerHealthGaps is the standing owner-interaction gap set (diagnostics).
func (s *Server) OwnerHealthGaps() []converge.OwnerGap {
	s.ownerMu.Lock()
	set := s.ownerHealthLocked().set
	s.ownerMu.Unlock()
	return set.Open()
}

// ownerActuator applies the recovery steps. Each one is a step, never a
// satisfaction: the gap it addresses stays standing until an observation
// says the desired state holds.
type ownerActuator struct{ s *Server }

func (a ownerActuator) Step(g converge.OwnerGap, kind converge.StepKind, now time.Time) error {
	switch kind {
	case converge.StepRequeueOwnerSend:
		return a.requeueOwnerSend(g, now)
	case converge.StepPublishLevelTruth:
		return a.publishLevelTruth(nil)
	case converge.StepACPUnstick:
		return a.acpUnstick()
	case converge.StepUXCoordinate:
		return a.publishLevelTruth(map[string]any{"ux": "yield_hydrate"})
	case converge.StepOverseerNoise:
		return a.overseerNoise(g, now)
	case converge.StepHumanAlert:
		return a.humanAlert(g, now)
	}
	return fmt.Errorf("owner health: unknown step %q", kind)
}

// requeueOwnerSend re-injects the owner's own words. This is 🎯T328
// owner-intent-resume generalized past restart: the same actuator serves a
// dropped queue, a failed delivery and a turn that evaporated mid-flight.
func (a ownerActuator) requeueOwnerSend(g converge.OwnerGap, now time.Time) error {
	a.s.ownerMu.Lock()
	h := a.s.ownerHealthLocked()
	text := h.sendText
	sealed := !h.replySealedAt.IsZero() && h.replySealedAt.After(h.ownerTurnAt)
	// Two dimensions can stand open over one lost turn; only the first of
	// them re-injects it this round.
	recent := h.requeuedID == h.sendID && !h.requeuedAt.IsZero() &&
		now.Sub(h.requeuedAt) < h.bounds.Reply
	if !recent {
		h.requeuedID, h.requeuedAt = h.sendID, now
	}
	a.s.ownerMu.Unlock()

	if strings.TrimSpace(text) == "" || sealed || recent {
		return converge.ErrOwnerStepNotApplicable
	}
	// Text still sitting in the notify queue will be delivered by the drain.
	// Re-injecting it would ask the owner's question twice; the gap stays
	// standing instead, and escalates if the queue never moves.
	wire := userTurnPrefix + text
	a.s.mu.RLock()
	queued := slices.Contains(a.s.notifyQueue, wire)
	a.s.mu.RUnlock()
	if queued {
		return converge.ErrOwnerStepNotApplicable
	}
	slog.Warn("owner health: re-injecting unacked owner turn",
		"kind", g.Kind, "reason", g.Reason, "attempt", g.StepsByKind[converge.StepRequeueOwnerSend]+1)
	return a.s.SendToOverseer(wire)
}

// publishLevelTruth re-publishes the server's working level so the chrome
// stops lying. The frame is deliberately not journaled: it is ephemeral
// chrome truth, not a turn. The idle text carries "settled" because the
// client only lets an idle level clear an open owner turn on a recovery or
// cancel wording (🎯T260).
func (a ownerActuator) publishLevelTruth(extra map[string]any) error {
	a.s.mu.RLock()
	clients := len(a.s.chatListeners)
	a.s.mu.RUnlock()
	if clients == 0 {
		return converge.ErrOwnerStepNotApplicable
	}

	working := a.s.overseerWorkingLevel()
	frame := map[string]any{"type": "status"}
	if working {
		frame["state"] = "thinking"
		frame["text"] = "turn in flight"
	} else {
		frame["state"] = "idle"
		frame["text"] = "chat settled — no turn in flight"
	}
	for k, v := range extra {
		frame[k] = v
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("owner health: marshal level frame: %w", err)
	}
	a.s.broadcastChatLive(string(payload))
	a.s.noteChromePublished(working)
	return nil
}

// acpUnstick interrupts a prompt that really is in flight — and only then.
// A stalled queue or a lost session is never fixed by an interrupt, so the
// precondition is re-checked at actuation time rather than trusted from the
// observation that opened the gap.
func (a ownerActuator) acpUnstick() error {
	proc := a.s.CurrentProcess()
	if proc == nil || !proc.Alive() || !proc.PromptInFlight() {
		return converge.ErrOwnerStepNotApplicable
	}
	slog.Warn("owner health: interrupting stalled owner prompt")
	if err := proc.Interrupt(); err != nil {
		return fmt.Errorf("owner health: interrupt: %w", err)
	}
	// Settle the server and the clients, then name the residual so the
	// reply dimension is satisfied by an outcome rather than by silence.
	a.s.settleCancel()
	a.s.NoteOwnerResidual("acp_unstuck")
	return nil
}

// overseerNoise reports an unrecovered owner-interaction gap to the overseer.
// It is sent as a system note, not an owner turn, so it never lights owner
// working chrome.
func (a ownerActuator) overseerNoise(g converge.OwnerGap, now time.Time) error {
	note := fmt.Sprintf(
		"[owner-interaction] %s gap unresolved: %s (%s) — open %s, %d steps taken. "+
			"The owner chat path is not converging; check delivery, chrome and reply on the daily seat.",
		g.Dimension, g.Kind, g.Reason, g.Age(now).Round(time.Second), g.StepCount())
	return a.s.SendToOverseer(note)
}

// humanAlert is the last rung: the owner is told, in their own chat, that
// interaction is degraded and recovery did not take. Journaled on purpose —
// a residual the owner can see is the honest end of an unrecoverable gap.
func (a ownerActuator) humanAlert(g converge.OwnerGap, now time.Time) error {
	text := fmt.Sprintf("owner interaction degraded: %s (%s) — unresolved for %s after %d recovery steps",
		g.Dimension, g.Kind, g.Age(now).Round(time.Second), g.StepCount())
	payload, err := json.Marshal(map[string]string{"type": "error", "error": text})
	if err != nil {
		return fmt.Errorf("owner health: marshal alert: %w", err)
	}
	slog.Error("owner health: unrecovered gap surfaced to the owner",
		"dimension", g.Dimension, "kind", g.Kind, "dwell", g.Age(now).String())
	a.s.BroadcastChat(string(payload))
	// 🎯T361: the client raises a sticky banner on this alert, so remember
	// that this dimension owes the owner a recovery notice.
	a.s.ownerMu.Lock()
	h := a.s.ownerHealthLocked()
	if h.alerted == nil {
		h.alerted = map[converge.OwnerDimension]bool{}
	}
	h.alerted[g.Dimension] = true
	a.s.ownerMu.Unlock()
	return nil
}
