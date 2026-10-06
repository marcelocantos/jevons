// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// Provider migration (🎯T691 / 🎯T1013.5). [claudia.Registry.Migrate] is
// the single decide-and-execute entry point: it decides whether a seat is
// live or stopped and performs the full mechanical sequence — context
// transfer, destination launch, seed hand-off — end to end. Jevons no
// longer orchestrates a separate prepare/thin-brief/seed/launch sequence
// on top of it (that decision tree used to live here and is what
// PrepareMigrationPinned reimplemented before 🎯T1013.5); it supplies the
// host-only inputs Claudia cannot know by itself — the session-root
// transcript lookup for an adopted seat, and the durable host journal for
// a predecessor with no provider transcript — and translates the uniform
// [claudia.SeatMigration] result back into the [handover.Pending] shape
// existing callers (jevons_agent_migrate, the overseer and fleet HTTP
// migrate routes) already understand.

// SetSessionRoots attaches the provider session stores used to resolve a
// predecessor's transcript (Grok sessions + Claude projects, 🎯T213).
func (f *Claudia) SetSessionRoots(r discovery.Roots) { f.roots = r }

// SetHandoverStore attaches the durable pending-handover store.
func (f *Claudia) SetHandoverStore(s *handover.Store) { f.handovers = s }

// SetSeatPlan attaches placement and migration fields the published
// AgentDef does not carry.
func (f *Claudia) SetSeatPlan(s *claudia.SeatPolicyStore) { f.seatPlans = s }

func (f *Claudia) seatState(name string) claudia.SeatPolicy {
	if f == nil || f.seatPlans == nil {
		return claudia.SeatPolicy{}
	}
	return f.seatPlans.Get(name)
}

// SetRetainedHistory supplies the durable host journal to Claudia when a
// live seat was adopted without process-local turns, or a stopped seat has
// no discoverable provider transcript path.
func (f *Claudia) SetRetainedHistory(read func(name string) (string, error)) {
	f.retainedHistory = read
}

// SetRotationStore attaches the durable last-rotation store (🎯T392.1.1).
func (f *Claudia) SetRotationStore(s *handover.RotationStore) { f.rotations = s }

// PendingHandovers is every record the store holds, oldest first. It is how
// the daemon's sweep finds a seed nobody delivered without knowing which
// agents ever migrated (🎯T418 clause 5): "pending for the next launch" is
// only a state with an owner if something reads the pending set.
//
// No store wired is an empty list rather than an error: a daemon without
// migration configured has no handovers to retry, which is not a fault.
func (f *Claudia) PendingHandovers() ([]handover.Pending, error) {
	if f == nil || f.handovers == nil {
		return nil, nil
	}
	return f.handovers.List()
}

// ClearHandover drops a record the sweep has finished with — delivered and
// past its double-seed window, or addressed to an agent that has left the
// fleet. Exposed here rather than reaching for the store directly so the
// registry-facing caller keeps one collaborator.
func (f *Claudia) ClearHandover(name string) error {
	if f == nil || f.handovers == nil {
		return nil
	}
	return f.handovers.Clear(name)
}

// PrepareMigration asks Claudia to move an agent to provider `to`.
//
// force performs the switch even when no predecessor transcript can be
// found — a deliberate cold start. Without it, an unfindable transcript
// refuses, because silently discarding an agent's history is exactly the
// outcome this path exists to prevent.
func (f *Claudia) PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error) {
	return f.PrepareMigrationPinned(name, to, "", force)
}

// PrepareMigrationPinned adds a destination model pin to the Claudia move.
// An empty model uses Claudia's destination selection.
//
// This is a thin wrapper over [claudia.Registry.Migrate] (🎯T1013.5): live
// vs. stopped dispatch, already-on-provider refusal, turn-in-flight refusal
// and force-interrupt-then-retry, and pending-destination retry all live in
// Claudia now. Jevons' job is the two inputs Claudia cannot supply itself —
// the 🎯T763 capability-drop refusal (checked before anything is stopped or
// persisted, same as the Stop+Register `rotate` path), and a best-effort
// retained transcript for a predecessor Claudia's own live/stopped sources
// cannot read — plus translating the result into the [handover.Pending]
// shape its callers already understand.
func (f *Claudia) PrepareMigrationPinned(name string, to claudia.Provider, model string, force bool) (handover.Pending, error) {
	if f == nil || f.reg == nil {
		return handover.Pending{}, fmt.Errorf("migrate: no agent registry")
	}
	target := cli.SubscriptionSeatProvider(claudia.Provider(strings.TrimSpace(string(to))))
	if target == "" {
		return handover.Pending{}, fmt.Errorf("migrate %q: target provider is required", name)
	}
	def := f.reg.Def(name)
	if def == nil {
		return handover.Pending{}, fmt.Errorf("migrate: no agent %q", name)
	}
	src := *def

	// 🎯T763: refuse before anything is stopped or persisted when the
	// switch would have to drop a restriction the new provider cannot
	// enforce. Claudia's Migrate has no opinion on jevons' capability
	// table, so this check stays at the host layer.
	if err := providerSwitchRefusal(src, target); err != nil {
		return handover.Pending{}, fmt.Errorf("migrate %q: %w", name, err)
	}

	// An old jevons-owned handover record must not linger once Claudia
	// owns the move: a stale pointer there is read by PendingHandover /
	// SeedSuccessor and would otherwise look like unfinished work.
	if f.handovers != nil {
		if err := f.handovers.Clear(name); err != nil {
			return handover.Pending{}, fmt.Errorf("migrate %q: clear obsolete host handover: %w", name, err)
		}
	}

	// Bracket the call with the host's launch hook only when the seat has
	// no live handle: a live seat migrates in place via Agent.Migrate and
	// nothing new comes up unwired, so the launch window would be a
	// decision Claudia already made, restated incorrectly.
	var endLaunch func()
	if proc := f.reg.Get(name); proc == nil || seatstate.ReadRegistry(f.reg, name).Alive == seatstate.No {
		endLaunch = f.launching(name)
	}
	retained := f.bestEffortRetainedTranscript(name, src)
	args := claudia.MigrateArgs{Provider: target, Model: model, Force: force, Reason: "explicit"}
	moved, err := f.reg.Migrate(context.Background(), name, args, retained)
	if endLaunch != nil {
		endLaunch()
	}

	pending := handover.Pending{
		Agent: name, From: string(src.Provider), To: string(target),
		Kind: handover.KindMigrate, OldSessionID: src.SessionID,
		BriefSource: "claudia-transfer/" + string(cli.PlanProvider(target)),
		Remap:       handover.RemapClaudiaMigrate,
	}
	if err != nil {
		return pending, fmt.Errorf("migrate %q: %w", name, err)
	}
	pending.Delivered = true // Claudia already seeded the successor.
	pending.NewSessionID = moved.Destination.SessionID
	pending.Model = moved.Destination.Model
	pending.Goal = moved.Destination.Goal
	pending.Purpose = moved.Destination.Purpose
	pending.WorkDir = moved.Destination.WorkDir
	pending.Parent = moved.Destination.Parent
	pending.TargetID = moved.Destination.TargetID
	pending.Brief = moved.Transfer.Brief
	f.noteModelSwitch(&ModelSwitch{
		Name: name, Provider: string(moved.Destination.Provider), FromProvider: string(src.Provider),
		From: src.Model, To: moved.Destination.Model, How: ModelSwitchHowMigrate,
	})
	f.logEvent("fleet_migrate", "remapped", map[string]any{
		"name": name, "to": string(moved.Destination.Provider), "from_provider": string(src.Provider),
		"from_model": src.Model, "to_model": moved.Destination.Model, "live": moved.Live,
	})
	slog.Info("agent session migrated via claudia Registry.Migrate",
		"name", name, "live", moved.Live, "from", pending.From, "to", pending.To,
		"old_session", pending.OldSessionID, "new_session", pending.NewSessionID)
	return pending, nil
}

// bestEffortRetainedTranscript supplies [claudia.Registry.Migrate]'s
// retainedTranscript argument when the host can find one: a sidecar/vendor
// transcript under the configured session roots, or (when the predecessor
// has no discoverable transcript path) the durable host journal. Errors
// here are not fatal to the migrate — Claudia's own live/stopped sources
// may already have enough to distill from — so a lookup failure returns ""
// rather than refusing the move; Force is Claudia's own refusal lever for
// "nothing to hand over at all".
func (f *Claudia) bestEffortRetainedTranscript(name string, def claudia.AgentDef) string {
	path := seatTranscript(def, f.roots)
	if path == "" {
		if f.retainedHistory == nil {
			return ""
		}
		history, err := f.retainedHistory(name)
		if err != nil {
			return ""
		}
		return history
	}
	history, err := migrationHistory(path)
	if err != nil {
		return ""
	}
	return history
}

func migrationHistory(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("no predecessor transcript")
	}
	logical, err := transcript.ReadLogical(path)
	if err != nil {
		return "", fmt.Errorf("read predecessor transcript: %w", err)
	}
	if len(logical.Turns) == 0 {
		return "", fmt.Errorf("predecessor transcript has no readable turns")
	}
	var history strings.Builder
	for _, turn := range logical.Turns {
		if text := strings.TrimSpace(turn.Text); text != "" {
			fmt.Fprintf(&history, "%s: %s\n", turn.Role, text)
		}
	}
	return history.String(), nil
}

// CompleteThinBrief remains for older host-owned handover records. Every
// current success path sets p.Remap = RemapClaudiaMigrate (🎯T1013.5:
// PrepareMigrationPinned always calls through claudia.Registry.Migrate),
// so this is reachable only by a record a pre-🎯T1013.5 build left on
// disk — Claudia has already run its disposable transfer for a
// Claudia-owned move, so that result returns here without another summary
// or destination turn.
func (f *Claudia) CompleteThinBrief(p handover.Pending) (handover.Pending, error) {
	return p, nil
}

// PrepareCompaction is withdrawn (🎯T40.2). A same-provider remint is
// not how a conversation continues and not how burn is controlled.
func (f *Claudia) PrepareCompaction(name string, force bool) (handover.Pending, error) {
	return handover.Pending{}, fmt.Errorf("compact %q: withdrawn (T40.2) — same-provider remint is not a product operation", name)
}

// rotate is the shared body of provider migration. kind colours the
// errors and the log line. Compaction no longer calls this (🎯T40.2), and
// neither does PrepareMigrationPinned since 🎯T1013.5 — it is exercised
// directly by the 🎯T763 capability-drop regression tests, which assert on
// the Stop+Register mechanics in isolation from a live provider backend.
func (f *Claudia) rotate(name string, target claudia.Provider, force bool, kind string) (handover.Pending, error) {
	def := f.reg.Def(name)
	if def == nil {
		return handover.Pending{}, fmt.Errorf("%s: no agent %q", kind, name)
	}
	// 🎯T763: refuse before anything is stopped or persisted when the switch
	// would have to drop a restriction the new provider cannot enforce.
	if err := providerSwitchRefusal(*def, target); err != nil {
		return handover.Pending{}, fmt.Errorf("%s %q: %w", kind, name, err)
	}

	// Resolve the pointer while the old session id is still on the row.
	oldSession := def.SessionID
	transcript := seatTranscript(*def, f.roots)
	if transcript == "" && !force {
		return handover.Pending{}, fmt.Errorf(
			"%s %q: no transcript found for session %s under the configured session roots — "+
				"its history cannot be handed over; pass force to switch cold anyway",
			kind, name, oldSession)
	}

	// Choose the successor session id before persisting: if a concurrent
	// reap Removes the row between Stop and Register, ensureRegistered's
	// MINT branch must recover THIS id from the handover, not uuid.New()
	// (🎯T474 — the jv-t444-phase-remint aside ghost).
	nextSession := uuid.NewString()
	var nextModel string
	if target == def.Provider {
		nextModel = cli.BindSessionModel(def.Model, target)
	} else {
		nextModel = cli.BindSessionModel("", target)
	}

	pending := handover.Pending{
		Agent:          name,
		From:           string(def.Provider),
		To:             string(target),
		Kind:           kind,
		OldSessionID:   oldSession,
		TranscriptPath: transcript,
		// Identity rides the handover so a bare-thread re-mint cannot
		// invent purpose=aside / empty workdir when the row is gone.
		Purpose:      def.Purpose,
		WorkDir:      def.WorkDir,
		Parent:       def.Parent,
		Model:        nextModel,
		TargetID:     def.TargetID,
		Goal:         def.Goal,
		NewSessionID: nextSession,
	}
	// Persist BEFORE rotation: after it, nothing else knows where the
	// predecessor's transcript is — or who the agent was (🎯T474).
	if f.handovers != nil {
		if err := f.handovers.Put(pending); err != nil {
			return handover.Pending{}, fmt.Errorf("%s %q: %w", kind, name, err)
		}
	}
	if f.rotations != nil {
		if err := f.rotations.Put(handover.Rotation{Agent: name, Kind: kind}); err != nil {
			return handover.Pending{}, fmt.Errorf("%s %q: persist last rotation: %w", kind, name, err)
		}
	}

	f.reg.Stop(name)

	fromModel := def.Model
	fromProvider := string(def.Provider)
	next := *def
	if err := switchProvider(&next, target, kind); err != nil {
		return handover.Pending{}, fmt.Errorf("%s %q: %w", kind, name, err)
	}
	next.SessionID = nextSession
	next.Model = nextModel
	next.Materialized = false // a fresh conversation, not a resume
	// def was snapshotted before Stop, which clears the serve endpoint on
	// the registry's own copy. Re-registering the snapshot would re-persist
	// a dead ConnectURL/PID and send the next Launch into a reattach that
	// resets (the 🎯T204 trap, here reached by a different road).
	next.ConnectURL = ""
	next.ConnectPID = 0
	if err := f.reg.Register(next); err != nil {
		return handover.Pending{}, fmt.Errorf("%s %q: register rotated row: %w", kind, name, err)
	}
	f.noteModelSwitch(&ModelSwitch{
		Name:         name,
		Provider:     string(next.Provider),
		FromProvider: fromProvider,
		From:         fromModel,
		To:           next.Model,
		How:          kind,
	})
	slog.Info("agent session rotation prepared",
		"kind", kind, "name", name, "from", pending.From, "to", pending.To,
		"old_session", oldSession, "new_session", next.SessionID,
		"transcript", transcript, "cold", transcript == "")
	return pending, nil
}

// PendingHandover returns the handover waiting for an agent, if any. The
// overseer's migration is driven by the HTTP server (it owns chat attach),
// so it reads the record here and seeds through its own send path.
func (f *Claudia) PendingHandover(name string) (handover.Pending, bool, error) {
	if f == nil || f.handovers == nil {
		return handover.Pending{}, false, nil
	}
	return f.handovers.Get(name)
}

// MarkHandoverDelivered records that a successor received its seed.
func (f *Claudia) MarkHandoverDelivered(name string) error {
	if f == nil || f.handovers == nil {
		return nil
	}
	return f.handovers.MarkDelivered(name)
}

// SeedSuccessor hands a freshly launched successor its one-off handover
// prompt, for an older host-owned handover record. Every current success
// path sets Pending.Remap = RemapClaudiaMigrate (🎯T1013.5), which Claudia
// already seeded before returning — a second host Deliver on top of that
// would be the bounce-nudge bug in another costume (🎯T646.1). ok=false
// means there was nothing to seed: the normal case for an agent that did
// not just migrate, a record already delivered, or — now always, for any
// record PrepareMigrationPinned can still produce — a Claudia-owned move.
func (f *Claudia) SeedSuccessor(name string) (handover.Pending, bool, error) {
	if f == nil || f.handovers == nil {
		return handover.Pending{}, false, nil
	}
	pending, ok, err := f.handovers.Get(name)
	if err != nil || !ok {
		return handover.Pending{}, false, err
	}
	return pending, false, nil
}

// seatTranscript is the handover pointer for def. Sidecar seats read
// ~/.jevons/spool, never a vendor JSONL (🎯T866.4).
func seatTranscript(def claudia.AgentDef, roots discovery.Roots) string {
	if spool.SidecarProvider(string(def.Provider)) {
		if path, err := spool.EnsureView(spool.Dir(), def.Name); err == nil && path != "" {
			return path
		}
	}
	return discovery.TranscriptPath(roots, def.SessionID)
}
