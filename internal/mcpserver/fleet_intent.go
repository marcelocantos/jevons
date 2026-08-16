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

	"github.com/marcelocantos/jevons/internal/fleetintent"
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

// OpenFleetIntent opens <stateDir>/fleet/intent.json and installs it.
//
// Reap stamps land at the existing T165/T195 call site (MarkAgentReaped).
// The later T435 RemovalAccount hook is the single chokepoint for every
// removal; this target does not take that dependency.
func (s *Server) OpenFleetIntent(stateDir string) error {
	st, err := fleetintent.Open(stateDir)
	if err != nil {
		return err
	}
	s.SetFleetIntentStore(st)

	snap := st.Snapshot()
	slog.Info("fleet intent store opened",
		"component", "fleet_intent",
		"path", st.Path(),
		"summary", snap.Summarize(),
	)
	return nil
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
		return nil
	}
	if err := store.SetAgent(name, st, by, reason, time.Now()); err != nil {
		return err
	}
	slog.Info("fleet intent set",
		"component", "fleet_intent",
		"name", name, "state", string(st), "by", by, "reason", reason,
	)
	return nil
}

// SetFleetIntent records a fleet-wide intent — a provider wall (🎯T406) or an
// owner standing the whole fleet down.
func (s *Server) SetFleetIntent(st fleetintent.State, by, reason string) error {
	store := s.intentStore()
	if store == nil {
		slog.Warn("fleet intent not recorded — no intent store installed",
			"component", "fleet_intent", "state", string(st), "by", by)
		return nil
	}
	if err := store.SetFleet(st, by, reason, time.Now()); err != nil {
		return err
	}
	slog.Info("fleet intent set (fleet-wide)",
		"component", "fleet_intent", "state", string(st), "by", by, "reason", reason,
	)
	return nil
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
}

// MarkAgentReaped stamps finished-and-reaped (🎯T165 / 🎯T195). 🎯T413 is the
// case this exists for: the registry row goes away, but a notifier holding a
// stale fleet view can still name the agent, and without a recorded intent
// that name reads as an idle worker somebody should restart.
func (s *Server) MarkAgentReaped(name, by, reason string) {
	if reason == "" {
		reason = "terminal report — finished and deregistered"
	}
	if err := s.SetAgentIntent(name, fleetintent.Reaped, by, reason); err != nil {
		slog.Warn("fleet intent reap stamp failed", "component", "fleet_intent", "name", name, "err", err)
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

// FleetIntentSummary is the owner-facing one-liner for the cockpit / logs.
func (s *Server) FleetIntentSummary() string { return s.fleetIntent().Summarize() }

// handleFleetIntent serves jevons_fleet_intent: read the whole intent state,
// or set one agent's / the fleet's.
//
// Reading is the default because the common operator question is "why is
// nothing restarting this?", and the answer is only useful if it is cheap to
// ask. Setting requires an explicit state, so a mistyped read can never
// silently stand an agent down.
func (s *Server) handleFleetIntent(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
	actor, _ := args["actor"].(string)
	if strings.TrimSpace(actor) == "" {
		actor = s.overseerName()
	}
	reason, _ := args["reason"].(string)
	reason = strings.TrimSpace(reason)

	if name == "" {
		if err := s.SetFleetIntent(st, actor, reason); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf(
			"Fleet-wide intent is now %s (by %s). %s\n\n%s",
			fleetintent.Describe(st), actor, fleetIntentEffect(st, "the fleet"),
			FormatFleetIntentReport(s.fleetIntent()))), nil
	}

	if err := s.SetAgentIntent(name, st, actor, reason); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf(
		"Intent for %q is now %s (by %s). %s",
		name, fleetintent.Describe(st), actor, fleetIntentEffect(st, name))), nil
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
