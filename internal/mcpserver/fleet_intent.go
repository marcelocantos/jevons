// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/attrib"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/reapverify"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T414: the daemon's half of the shared intent representation. The pure
// policy and the durable store live in internal/fleetintent; this file is
// where the daemon opens the store, hands snapshots to the controls, and
// stamps intent when a deliberate act occurs.
//
// The stamping matters as much as the reading. A representation nothing
// writes to is a representation every control reads as "working", which is
// exactly the state of the world on 2026-08-10.

// SetFleetIntentStore installs the durable intent store (nil disables, which
// resolves every read to working — the pre-T414 behaviour).
func (s *Server) SetFleetIntentStore(st *fleetintent.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intent = st
}

// OpenFleetIntent opens <stateDir>/fleet/intent.json, installs it, and
// attaches the one stamping hook the daemon cannot do without.
//
// Call it after [Server.SetRemovalAccount], so the hook lands on the
// process-wide 🎯T435 Account rather than on a private one this call would
// otherwise create.
func (s *Server) OpenFleetIntent(stateDir string) error {
	st, err := fleetintent.Open(stateDir)
	if err != nil {
		return err
	}
	s.SetFleetIntentStore(st)

	// 🎯T753: owed achieve-or-reopen decisions for reaped implementers
	// survive a restart alongside the intent they qualify.
	rv, err := reapverify.Open(stateDir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.reapVerify = rv
	s.mu.Unlock()

	// Every accounted removal is a deliberate decision that this name should
	// not be running, so every accounted removal stamps it. One hook on the
	// chokepoint rather than a stamp at each of the eight removal call sites
	// across two packages: a per-caller convention is exactly the arrangement
	// that produced three subsystems with three different answers.
	s.RemovalAccount().SetBeforeRemoveHook(s.onAccountedRemoving)
	s.RemovalAccount().SetRemovedHook(s.onAccountedRemoval)

	snap := st.Snapshot()
	slog.Info("fleet intent store opened",
		"component", "fleet_intent",
		"path", st.Path(),
		"summary", snap.Summarize(),
	)
	return nil
}

// onAccountedRemoving runs while the row still exists (🎯T708): detached
// background load is signalled before Registry.Remove drops the pid. It
// does not kill a quiet seat root — that would interrupt an in-flight
// write (🎯T597 / 🎯T702). Leftover commits are 🎯T734's after-hook.
func (s *Server) onAccountedRemoving(name string) {
	s.reapSeatLoad(name)
}

// onAccountedRemoval is the process-wide 🎯T435 hook: stamp finished-and-reaped
// so a stale fleet view cannot restart the name, and arm the 🎯T734 watch so
// a leftover pane that keeps committing is attributed to the parent.
func (s *Server) onAccountedRemoval(name string, rm fleetlog.Removal) {
	by := strings.TrimSpace(rm.Actor)
	if by == "" {
		reason := strings.TrimSpace(rm.Reason)
		if reason == "" {
			reason = fleetlog.ReasonUnaccounted
		}
		by = "product:" + reason
	}
	s.MarkAgentReapedWithCause(name, by, strings.TrimSpace(rm.Detail), strings.TrimSpace(rm.Reason))
	s.armPostReapCommitWatch(name, rm)
}

// intentStore returns the installed store (nil-safe).
func (s *Server) intentStore() *fleetintent.Store {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.intent
}

// fleetIntent is the snapshot every control reads. A nil server or nil store
// yields an all-working snapshot, so no call site needs a nil check.
func (s *Server) fleetIntent() fleetintent.Snapshot {
	return s.intentStore().Snapshot()
}

// AllowFleetControl is the one question a control asks: may control c act on
// this agent? The Decision carries the intent so the caller can name it.
func (s *Server) AllowFleetControl(name string, c fleetintent.Control) fleetintent.Decision {
	return s.fleetIntent().Allow(name, c)
}

// SetAgentIntent records a deliberate intent for one agent and logs it. by is
// the actor (owner, overseer, a product path such as the 🎯T165 reap).
func (s *Server) SetAgentIntent(name string, st fleetintent.State, by, reason string) error {
	store := s.intentStore()
	if store == nil {
		// Said out loud rather than silently dropped: without a state dir the
		// daemon cannot remember a park across the restart that would undo it.
		slog.Warn("fleet intent not recorded — no intent store installed",
			"component", "fleet_intent", "name", name, "state", string(st), "by", by)
		s.kickOverseerNotifyDrain()
		return nil
	}
	if err := store.SetAgent(name, st, by, reason, time.Now()); err != nil {
		return err
	}
	slog.Info("fleet intent set",
		"component", "fleet_intent",
		"name", name, "state", string(st), "by", by, "reason", reason,
	)
	s.kickOverseerNotifyDrain()
	return nil
}

// SetFleetIntent records a fleet-wide intent — a provider wall (🎯T406) or an
// owner standing the whole fleet down.
func (s *Server) SetFleetIntent(st fleetintent.State, by, reason string) error {
	store := s.intentStore()
	if store == nil {
		slog.Warn("fleet intent not recorded — no intent store installed",
			"component", "fleet_intent", "state", string(st), "by", by)
		s.kickOverseerNotifyDrain()
		return nil
	}
	if err := store.SetFleet(st, by, reason, time.Now()); err != nil {
		return err
	}
	slog.Info("fleet intent set (fleet-wide)",
		"component", "fleet_intent", "state", string(st), "by", by, "reason", reason,
	)
	s.kickOverseerNotifyDrain()
	return nil
}

// SetOverseerNotifyDrain installs the hook that runs after an intent write.
// Production wires it to the HTTP server's notify drain so a held overseer
// queue leaves once the fleet is runnable. Nil disables the kick.
func (s *Server) SetOverseerNotifyDrain(fn func()) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.overseerNotifyDrain = fn
	s.mu.Unlock()
}

// kickOverseerNotifyDrain runs the installed hook without holding s.mu.
// The drain's own gate no-ops while intent still refuses a turn.
func (s *Server) kickOverseerNotifyDrain() {
	if s == nil {
		return
	}
	s.mu.Lock()
	fn := s.overseerNotifyDrain
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// ForgetAgentIntent drops a stored intent when the name leaves the registry.
func (s *Server) ForgetAgentIntent(name string) {
	if err := s.intentStore().Forget(name); err != nil {
		slog.Warn("fleet intent forget failed", "component", "fleet_intent", "name", name, "err", err)
	}
}

// MarkAgentParked stamps a deliberate stop (🎯T408): stopping without killing
// is an instruction, and it has to outlive both the delivery that would
// restart the agent and the daemon restart that would reattach it.
func (s *Server) MarkAgentParked(name, by, reason string) {
	if reason == "" {
		reason = "deliberate stop"
	}
	if err := s.SetAgentIntent(name, fleetintent.Parked, by, reason); err != nil {
		slog.Warn("fleet intent park failed", "component", "fleet_intent", "name", name, "err", err)
	}
	s.drainSharedIndex(name)
}

// MarkAgentReaped stamps finished-and-reaped (🎯T165 / 🎯T195). 🎯T413 is the
// case this exists for: the registry row goes away, but a notifier holding a
// stale fleet view can still name the agent, and without a recorded intent
// that name reads as an idle worker somebody should restart.
func (s *Server) MarkAgentReaped(name, by, reason string) {
	s.MarkAgentReapedWithCause(name, by, reason, "")
}

// MarkAgentReapedWithCause is [MarkAgentReaped] plus the structured
// stop_reason category (🎯T972): the fleetlog.Reason* vocabulary value that
// distinguishes a finished-work reap (reap_done / reap_achieve) from any
// other removal cause (kill, dead_seat, startup_stall, unbriefed_seat, ...).
// Every accounted removal reaches this through onAccountedRemoval, so every
// reap — not only the 🎯T165/🎯T195 finish path — carries its cause.
func (s *Server) MarkAgentReapedWithCause(name, by, reason, stopReason string) {
	if reason == "" {
		reason = "terminal report — finished and deregistered"
	}
	store := s.intentStore()
	if store == nil {
		slog.Warn("fleet intent not recorded — no intent store installed",
			"component", "fleet_intent", "name", name, "state", string(fleetintent.Reaped), "by", by)
		s.drainSharedIndex(name)
		return
	}
	if err := store.SetAgentReaped(name, by, reason, stopReason, time.Now()); err != nil {
		slog.Warn("fleet intent reap stamp failed", "component", "fleet_intent", "name", name, "err", err)
	} else {
		slog.Info("fleet intent set",
			"component", "fleet_intent",
			"name", name, "state", string(fleetintent.Reaped), "by", by, "reason", reason, "stop_reason", stopReason,
		)
	}
	s.drainSharedIndex(name)
}

// drainSharedIndex rides the intent stamps because they are the one place
// every deliberate stop already passes through (🎯T408/T165): an agent that
// stops must leave the shared index empty, with whatever was staged saved
// and attributed first (🎯T466). A name the registry no longer holds — a
// reap stamped after removal — is a silent no-op; the drain in
// fleet.Claudia.Remove ran while the definition still existed.
func (s *Server) drainSharedIndex(name string) {
	if s == nil || s.registry == nil {
		return
	}
	if def := s.registry.Def(name); def != nil {
		attrib.DrainOnStop(def.WorkDir, def.SessionID, name)
	}
}

// MarkAgentWorking clears a park or block. This is the only way a stood-down
// agent becomes eligible again: nothing infers it from a process, a timer, or
// a control's own impatience.
func (s *Server) MarkAgentWorking(name, by, reason string) {
	if reason == "" {
		reason = "resumed"
	}
	if err := s.SetAgentIntent(name, fleetintent.Working, by, reason); err != nil {
		slog.Warn("fleet intent resume failed", "component", "fleet_intent", "name", name, "err", err)
	}
}

// ensureFreshStartIntent is the single 🎯T414/🎯T413/🎯T1109 gate every start
// path must pass through before registering or launching a process: should
// this name be running at all? A standing park or provider wall refuses the
// start here, naming the intent — except Reaped, which 🎯T560's kill leaves
// behind on a preserved row precisely so the name can be reused. A fresh
// start under a reaped name is the ordinary case (not a revival of the old
// stopped process — Launch always runs below any caller of this helper), so
// here the intent is reset to Working before the caller registers/launches.
// Skipping this reset is exactly the 🎯T1109 bug: a caller that registers and
// launches a process without going through this door leaves the registry
// row's intent at Reaped under a live process, and the 🎯T984 sentinel then
// stands the seat back down within its next sweep, reading the stale intent
// as ground truth. Every start path — jevons_agent_start's own handler,
// frontier_consume's auto-spawn, and any future caller — MUST call this (or
// MarkAgentWorking directly, as the 🎯T530 restart-to-drain path already
// does) before registering a name that might be a previously-reaped row.
func (s *Server) ensureFreshStartIntent(name, actor string) error {
	if dec := s.AllowFleetControl(name, fleetintent.ControlSpawn); !dec.Allow {
		if dec.Blocking != fleetintent.Reaped {
			return fmt.Errorf(
				"refusing to start %q — %s (%s). Lift it with jevons_fleet_intent state=working before starting.",
				name, fleetintent.Describe(dec.Blocking), dec.Reason)
		}
		s.MarkAgentWorking(name, actor, "fresh start under a previously reaped name")
	}
	return nil
}

// FleetIntentSummary is the owner-facing one-liner for the cockpit / logs.
func (s *Server) FleetIntentSummary() string { return s.fleetIntent().Summarize() }

// handleFleetIntent serves jevons_fleet_intent: read the whole intent state,
// or set one agent's / the fleet's.
//
// Reading is the default because the common operator question is "why is
// nothing restarting this?", and the answer is only useful if it is cheap to
// ask. Setting requires an explicit state, so a mistyped read can never
// silently stand an agent down.
func (s *Server) handleFleetIntent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["name"].(string)
	name = strings.TrimSpace(name)
	raw, _ := args["state"].(string)
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return mcp.NewToolResultText(FormatFleetIntentReport(s.fleetIntent())), nil
	}

	st, err := fleetintent.Parse(raw)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// 🎯T969: an actor-less change is recorded as the caller this request can
	// identify, or "unattributed" — never as the overseer by default.
	actor := s.intentActor(ctx, args)
	note := ""
	if actorWasInferred(args) {
		note = inferredActorNote(actor)
	}
	reason, _ := args["reason"].(string)
	reason = strings.TrimSpace(reason)

	if name == "" {
		if err := s.SetFleetIntent(st, actor, reason); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"Fleet-wide intent is now %s (by %s). %s%s\n\n%s",
			fleetintent.Describe(st), actor, fleetIntentEffect(st, "the fleet"), note,
			FormatFleetIntentReport(s.fleetIntent()))), nil
	}

	if err := s.SetAgentIntent(name, st, actor, reason); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	stopped := s.nameLooksStopped(name)
	return mcp.NewToolResultText(fmt.Sprintf(
		"Intent for %q is now %s (by %s). %s%s%s",
		name, fleetintent.Describe(st), actor, fleetIntentEffect(st, name), note,
		fleetIntentRevivalCaveat(st, stopped))), nil
}

// nameLooksStopped reports that name is a registered row with no live
// process right now — the shape 🎯T560 preserves after a supervisor kill.
// Best-effort: a nil registry or unknown name reads as not-stopped so the
// caveat only ever adds information, never a false alarm.
func (s *Server) nameLooksStopped(name string) bool {
	if s == nil || s.registry == nil {
		return false
	}
	if s.registry.Def(name) == nil {
		return false
	}
	return s.seatState(name).Alive != seatstate.Yes
}

// fleetIntentRevivalCaveat is 🎯T1112's clarifying sentence: lifting a
// stopped row's intent to working removes the decline, but it is not itself
// a spawn, a send, or a revive — nothing mints a process from this call
// alone. An operator clearing stale intents should not be surprised by a
// fleet six minutes later.
func fleetIntentRevivalCaveat(st fleetintent.State, stopped bool) string {
	if !stopped || !fleetintent.Runnable(st) {
		return ""
	}
	return " This row stays stopped until spawned or sent to (🎯T1112) — lifting intent only clears the decline; it does not start the process."
}

// fleetIntentEffect states the consequence in the caller's terms. An intent
// nobody understands the effect of is set wrongly, and the whole point of
// 🎯T414 is that the effect is uniform across every control rather than one
// subsystem's private behaviour.
func fleetIntentEffect(st fleetintent.State, subject string) string {
	if fleetintent.Runnable(st) {
		return "Every control may act on " + subject + " again: spawn, nudge, revive, repressure, repair and delivery-start are all unblocked."
	}
	return "No control will start, nudge, revive, repressure, repair or deliver-start " + subject +
		" while this stands — each declines naming the intent. Lift it with state=working."
}

// FormatFleetIntentReport renders the full intent state for an operator.
func FormatFleetIntentReport(snap fleetintent.Snapshot) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "Fleet intent: %s\n", snap.Fleet.Describe())
	held := snap.NotWorking()
	if len(held) == 0 {
		b.WriteString("No agent carries a non-working intent.\n")
		return b.String()
	}
	fmt.Fprintf(b, "%d agent(s) stood down:\n", len(held))
	for _, name := range held {
		fmt.Fprintf(b, "  %s — %s\n", name, snap.Agents[name].Describe())
	}
	return b.String()
}
