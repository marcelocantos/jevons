// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/handover"
)

// Provider migration (🎯T285).
//
// Moving an existing agent to another backend cannot preserve its
// session — the stores are per-provider and claudia fails closed on a
// session id it cannot find — so the agent is rotated onto a fresh
// session and its successor is pointed at the predecessor's transcript.
//
// The order matters and is the whole reason this is not two lines at a
// call site: the transcript pointer must be resolved and PERSISTED
// before the registry row is rotated, because rotation overwrites the
// old session id and nothing else remembers it.

// SetSessionRoots attaches the provider session stores used to resolve a
// predecessor's transcript (Grok sessions + Claude projects, 🎯T213).
func (f *Claudia) SetSessionRoots(r discovery.Roots) { f.roots = r }

// SetHandoverStore attaches the durable pending-handover store.
func (f *Claudia) SetHandoverStore(s *handover.Store) { f.handovers = s }

// PrepareMigration rotates an agent onto a new session under provider
// `to`, after recording where its predecessor's transcript lives. It does
// NOT launch: the caller launches and then calls SeedSuccessor, so a
// failed launch leaves a pending handover on disk rather than a half
// migration with the pointer lost.
//
// force performs the switch even when no predecessor transcript can be
// found — a deliberate cold start. Without it, an unfindable transcript
// refuses, because silently discarding an agent's history is exactly the
// outcome this path exists to prevent.
func (f *Claudia) PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error) {
	if f == nil || f.reg == nil {
		return handover.Pending{}, fmt.Errorf("migrate: no agent registry")
	}
	target := claudia.Provider(strings.TrimSpace(string(to)))
	if target == "" {
		return handover.Pending{}, fmt.Errorf("migrate %q: target provider is required", name)
	}
	def := f.reg.Def(name)
	if def == nil {
		return handover.Pending{}, fmt.Errorf("migrate: no agent %q", name)
	}
	if def.Provider == target {
		return handover.Pending{}, fmt.Errorf("migrate %q: already on %s", name, target)
	}
	return f.rotate(name, target, force, "migrate")
}

// PrepareCompaction rotates an agent onto a fresh session on the SAME
// provider, handing the successor its predecessor's transcript (🎯T392.1).
//
// This is the context ceiling's only enforcement action, and it is the
// migration path with the provider held constant — deliberately, because
// the hard part of both is identical: resolve and persist the transcript
// pointer before the rotation overwrites the session id that finds it.
// Asking the model to compact itself was the alternative and it is not
// one, since a model deciding when its own context is too large is the
// judgement call the ceiling exists to replace.
//
// The cost of a compaction is one turn at the ceiling plus the re-reads
// that follow it. That is charged knowingly: a replay of the 🎯T392
// baseline puts a 100k ceiling at -63% net of 56 such compactions.
func (f *Claudia) PrepareCompaction(name string, force bool) (handover.Pending, error) {
	if f == nil || f.reg == nil {
		return handover.Pending{}, fmt.Errorf("compact: no agent registry")
	}
	def := f.reg.Def(name)
	if def == nil {
		return handover.Pending{}, fmt.Errorf("compact: no agent %q", name)
	}
	return f.rotate(name, def.Provider, force, "compact")
}

// rotate is the shared body of migration and compaction. kind only
// colours the errors and the log line; the mechanics are the same.
func (f *Claudia) rotate(name string, target claudia.Provider, force bool, kind string) (handover.Pending, error) {
	def := f.reg.Def(name)
	if def == nil {
		return handover.Pending{}, fmt.Errorf("%s: no agent %q", kind, name)
	}

	// Resolve the pointer while the old session id is still on the row.
	oldSession := def.SessionID
	transcript := discovery.TranscriptPath(f.roots, oldSession)
	if transcript == "" && !force {
		return handover.Pending{}, fmt.Errorf(
			"%s %q: no transcript found for session %s under the configured session roots — "+
				"its history cannot be handed over; pass force to switch cold anyway",
			kind, name, oldSession)
	}

	pending := handover.Pending{
		Agent:          name,
		From:           string(def.Provider),
		To:             string(target),
		OldSessionID:   oldSession,
		TranscriptPath: transcript,
	}
	// Persist BEFORE rotation: after it, nothing else knows where the
	// predecessor's transcript is.
	if f.handovers != nil {
		if err := f.handovers.Put(pending); err != nil {
			return handover.Pending{}, fmt.Errorf("%s %q: %w", kind, name, err)
		}
	}

	f.reg.Stop(name)

	next := *def
	next.Provider = target
	next.SessionID = uuid.NewString()
	next.Materialized = false // a fresh conversation, not a resume
	// 🎯T324: session-truth binding is rewritten for the new SessionID.
	// Never inherit the previous provider's pin (e.g. "fable" under Claude
	// → Grok). Bind the target provider's default so cold agents get a
	// condensable model, not a bare mark forever (T323 residual cleared
	// the pin only; fail-closed sniff is not the product strategy).
	// A cross-provider move must not inherit the old provider's pin (e.g.
	// "fable" under Claude → Grok). A same-provider compaction must keep
	// it: the agent is the same agent on the same backend, and silently
	// re-defaulting its model would make a context rotation change what
	// the agent is.
	if target == def.Provider {
		next.Model = cli.BindSessionModel(def.Model, target)
	} else {
		next.Model = cli.BindSessionModel("", target)
	}
	// def was snapshotted before Stop, which clears the serve endpoint on
	// the registry's own copy. Re-registering the snapshot would re-persist
	// a dead ConnectURL/PID and send the next Launch into a reattach that
	// resets (the 🎯T204 trap, here reached by a different road).
	next.ConnectURL = ""
	next.ConnectPID = 0
	if err := f.reg.Register(next); err != nil {
		return handover.Pending{}, fmt.Errorf("%s %q: register rotated row: %w", kind, name, err)
	}
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
// prompt. Returns ok=false when there was nothing pending (the normal case
// for an agent that did not just migrate) or when the record was already
// delivered, so a resumed migration cannot seed twice.
//
// The turn is dispatched asynchronously through Deliver, which waits for
// the reply. Both halves of that matter:
//
//   - Asynchronous, because reading a predecessor's transcript can take
//     minutes and the caller (an MCP tool call) must not block on it.
//   - Through Deliver rather than a bare Send, because a fire-and-forget
//     send suits a Claude TUI but breaks Grok's ACP request/response
//     cycle: nothing consumes the response, and the next prompt fails the
//     session with a bare "Internal error" (observed migrating claude →
//     grok before this was fixed).
//
// The record is marked delivered only once the turn actually completes,
// so a failed hand-off stays pending for the next launch.
//
// 🎯T416 — THIS IS THE FOURTH CALLER OF THE STUCK SEND PATH, and the only one
// that was already telling the truth.
//
// The other three (deliverToSender, deliverToOverseer, drainAgentSendQueue)
// inferred success from proc.Send() returning nil and reported "Message sent"
// for a payload that never left the composer. This one waits for the reply, so
// a paste that is never submitted times out and says so, in handOffSeed's
// ERROR line. On 2026-08-10 at 18:21 that line was emitted verbatim for
// jv-t416-send-turn-begin — a true delivery failure, correctly reported, that
// nobody read. Every instrument consulted that day lied; the one that was
// honest was unconsulted.
//
// So this arm needs no honesty added, and the fix is to bring the other three
// up to it rather than to touch it. What it still lacks is RECOVERY: "it stays
// pending for the next launch" is why that worker sat dark for 43 minutes, and
// a launch that may never come is not a retry. That half is 🎯T418 clause 5 and
// is deliberately not done here.
func (f *Claudia) SeedSuccessor(name string) (handover.Pending, bool, error) {
	if f == nil || f.handovers == nil {
		return handover.Pending{}, false, nil
	}
	pending, ok, err := f.handovers.Get(name)
	if err != nil || !ok {
		return handover.Pending{}, false, err
	}
	if !pending.Usable() {
		return pending, false, nil
	}
	ag := f.reg.Get(name)
	if ag == nil || !ag.Alive() {
		// Leave the record pending: the next successful launch delivers it.
		return pending, false, fmt.Errorf("seed %q: no live process to hand the transcript to", name)
	}

	go f.handOffSeed(name, pending)
	slog.Info("handover dispatched", "detail", pending.Describe())
	return pending, true, nil
}

// handOffSeed is the dispatched turn, extracted from SeedSuccessor's goroutine
// so the fail-closed arm can be exercised without a live provider process
// (🎯T416 clause 9, instrument A). The suite asserts on the ERROR line itself,
// because that line IS the instrument: it is what an operator would have had to
// read to catch the 18:21 hand-off failure at the time, and an instrument
// nothing asserts on is one the next refactor quietly drops.
func (f *Claudia) handOffSeed(name string, pending handover.Pending) {
	if _, err := f.deliverSeed(name, pending.Seed()); err != nil {
		slog.Error("handover hand-off failed; it stays pending for the next launch",
			"name", name, "err", err)
		return
	}
	if err := f.handovers.MarkDelivered(name); err != nil {
		slog.Error("handover delivered but not marked — successor may be seeded twice",
			"name", name, "err", err)
	}
	slog.Info("handover delivered", "detail", pending.Describe())
}

// deliverSeed is how the seed reaches the successor: Deliver on the product
// path, overridable for the oracle. A test seam rather than an option — nothing
// but a test ever sets it.
func (f *Claudia) deliverSeed(name, seed string) (string, error) {
	if f.seedDeliver != nil {
		return f.seedDeliver(name, seed)
	}
	return f.Deliver(name, seed)
}
