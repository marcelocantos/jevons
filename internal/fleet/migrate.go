// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/seatplan"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// Provider migration (🎯T691). Claudia owns the provider switch and durable
// handover: Agent.Migrate handles live seats, and Registry.MigrateStopped
// handles seats with no live process. Both use a disposable transfer agent
// before starting the destination work session. Jevons supplies normalized
// history for a stopped seat and recovers handover records written by older
// builds; it does not write a second record for a Claudia-owned move.

// SetSessionRoots attaches the provider session stores used to resolve a
// predecessor's transcript (Grok sessions + Claude projects, 🎯T213).
func (f *Claudia) SetSessionRoots(r discovery.Roots) { f.roots = r }

func (f *Claudia) prepareMigrationBrief(def claudia.AgentDef, destination claudia.Provider, path string) (string, claudia.Provider, error) {
	history, err := migrationHistory(path)
	if err != nil {
		return "", "", err
	}
	provider := cli.PlanProvider(destination)
	args := MigrationTransferArgs{
		Destination: destination, Goal: def.Goal, Transcript: history,
	}
	var result MigrationTransferResult
	if f.migrationTransfer != nil {
		result, err = f.migrationTransfer(args)
	} else {
		err = fmt.Errorf("migration summary is not in the published claudia module")
	}
	if err != nil {
		return "", provider, err
	}
	if strings.TrimSpace(result.Brief) == "" {
		return "", provider, fmt.Errorf("transfer agent returned an empty brief")
	}
	return result.Brief, provider, nil
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

// SetHandoverStore attaches the durable pending-handover store.
func (f *Claudia) SetHandoverStore(s *handover.Store) { f.handovers = s }

// SetSeatPlan attaches placement and migration fields the published
// AgentDef does not carry.
func (f *Claudia) SetSeatPlan(s *seatplan.Store) { f.seatPlans = s }

func (f *Claudia) seatState(name string) seatplan.State {
	if f == nil || f.seatPlans == nil {
		return seatplan.State{}
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

// seatInFlight asks the shared seat-state authority (🎯T766.2, census
// derivation 5) whether a turn is running, instead of migrate deriving its
// own answer from a bare .PromptInFlight() nobody else sees. Unknown (no
// authority wired, or the seat has never been observed) is read as not
// in-flight: a migrate that cannot tell is not the caller that should
// invent a false positive and refuse to move a quiescent seat.
func (f *Claudia) seatInFlight(name string, live *claudia.Agent) bool {
	if live == nil {
		return false
	}
	if f != nil && f.seats != nil {
		if st, ok := f.seats.Get(name); ok {
			return st.InFlight == seatstate.Yes
		}
	}
	return false
}

// PrepareMigration asks Claudia to move an agent to provider `to`. A live
// handle moves in place; a stopped seat is transferred, persisted, and
// launched by Claudia. The returned Pending is a compatibility result for
// Jevons callers, not a second host-owned handover record.
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
	if cli.PlanProvider(def.Provider) == cli.PlanProvider(target) && f.seatState(name).MigrationSeed == "" {
		return handover.Pending{}, fmt.Errorf("migrate %q: already on %s", name, target)
	}
	if f.seatState(name).MigrationSeed != "" {
		return f.migrateStoppedViaClaudia(name, *def, target, model, force)
	}
	if live := f.reg.Get(name); live != nil && live.Alive() {
		if f.seatInFlight(name, live) && !force {
			return handover.Pending{}, fmt.Errorf("migrate %q: turn in flight; wait or interrupt before context transfer", name)
		}
		draft := handover.Pending{
			Agent: name, From: string(def.Provider), To: string(target),
			Kind: handover.KindMigrate, OldSessionID: def.SessionID,
			BriefSource: "claudia-transfer/" + string(cli.PlanProvider(target)),
		}
		if cli.PlanProvider(live.Provider()) == cli.PlanProvider(target) {
			// Claudia may have moved the live process while this host's row
			// still names the source. Reconcile that move without paying for
			// a second transfer summary or minting another destination.
			if pending, ok, err := f.remapViaClaudia(name, target, model, force, draft); ok {
				return pending, err
			}
			return handover.Pending{}, fmt.Errorf("migrate %q: live destination could not be reconciled", name)
		}
		// Claudia performs the disposable transfer and switch. Jevons supplies
		// its durable transcript because an adopted live handle may have no
		// process-local turns after a daemon restart.
		if pending, ok, err := f.remapViaClaudia(name, target, model, force, draft); ok {
			return pending, err
		}
	}
	if f.liveMigrate != nil {
		// Test-only injected live seam: the fixture has no process handle,
		// but still exercises the live Migrate refusal and reconciliation.
		draft := handover.Pending{
			Agent: name, From: string(def.Provider), To: string(target),
			Kind: handover.KindMigrate, OldSessionID: def.SessionID,
			TranscriptPath: seatTranscript(*def, f.roots),
		}
		brief, provider, err := f.prepareMigrationBrief(*def, target, draft.TranscriptPath)
		if err != nil {
			return handover.Pending{}, fmt.Errorf("migrate %q: context transfer: %w", name, err)
		}
		draft.Brief = brief
		draft.BriefSource = "claudia-transfer/" + string(provider)
		if pending, ok, err := f.remapViaClaudia(name, target, model, force, draft); ok {
			return pending, err
		}
	}

	return f.migrateStoppedViaClaudia(name, *def, target, model, force)
}

func (f *Claudia) migrateStoppedViaClaudia(name string, def claudia.AgentDef, target claudia.Provider, model string, force bool) (handover.Pending, error) {
	if f.handovers != nil {
		if err := f.handovers.Clear(name); err != nil {
			return handover.Pending{}, fmt.Errorf("migrate %q: clear obsolete host handover: %w", name, err)
		}
	}
	var history string
	pendingMig := f.seatState(name).MigrationSeed
	if pendingMig == "" {
		path := seatTranscript(def, f.roots)
		var err error
		if path == "" && f.retainedHistory != nil {
			history, err = f.retainedHistory(name)
		} else {
			history, err = migrationHistory(path)
		}
		if err != nil {
			return handover.Pending{}, fmt.Errorf("migrate %q: predecessor context: %w", name, err)
		}
	}
	args := claudia.MigrateArgs{
		Provider: target, Model: model, Force: force, Reason: "explicit",
	}
	var result StoppedMigration
	var err error
	if f.stoppedMigrate != nil {
		result, err = f.stoppedMigrate(name, args, history)
	} else {
		return handover.Pending{}, fmt.Errorf("migrate %q: stopped migration is not in the published claudia module", name)
	}
	fromProvider, fromSession := def.Provider, def.SessionID
	mig := f.seatState(name)
	if mig.MigrationFrom != "" {
		fromProvider, fromSession = mig.MigrationFrom, mig.MigrationFromSession
	}
	pending := handover.Pending{
		Agent: name, From: string(fromProvider), To: string(target),
		Kind: handover.KindMigrate, OldSessionID: fromSession,
		BriefSource: "claudia-transfer/" + string(cli.PlanProvider(target)),
		Remap:       handover.RemapClaudiaMigrate,
	}
	if result.Destination.SessionID != "" {
		pending.NewSessionID = result.Destination.SessionID
	}
	pending.Brief = result.Transfer.Brief
	if err != nil {
		return pending, err
	}
	pending.Delivered = true
	pending.Model = result.Destination.Model
	pending.Goal = result.Destination.Goal
	pending.Purpose = result.Destination.Purpose
	pending.WorkDir = result.Destination.WorkDir
	pending.Parent = result.Destination.Parent
	pending.TargetID = result.Destination.TargetID
	f.noteModelSwitch(&ModelSwitch{
		Name: name, Provider: string(result.Destination.Provider), FromProvider: string(fromProvider),
		From: def.Model, To: result.Destination.Model, How: ModelSwitchHowMigrate,
	})
	return pending, nil
}

// CompleteThinBrief remains for older host-owned handover records. Claudia
// has already run its disposable transfer for a Claudia-owned move, so that
// result returns here without another summary or destination turn.
func (f *Claudia) CompleteThinBrief(p handover.Pending) (handover.Pending, error) {
	if p.Remap == handover.RemapClaudiaMigrate {
		return p, nil
	}
	if strings.HasPrefix(p.BriefSource, "claudia-transfer/") {
		return p, nil
	}
	if !handover.ProviderSwitch(p.From, p.To) {
		return p, nil
	}
	if p.BriefSource == string(handover.SourceSelf) || strings.TrimSpace(p.CompactSessionID) != "" {
		return p, nil
	}
	if !handover.DistillTooThin(p.Brief) && !handover.DistillTooThin(handover.Distill(p.TranscriptPath)) {
		return p, nil
	}
	sid, text, err := f.runThrowawayCompact(p)
	if err != nil || strings.TrimSpace(text) == "" {
		return p, nil
	}
	p.Brief = strings.TrimSpace(text)
	p.BriefSource = string(handover.SourceCompact)
	p.CompactSessionID = strings.TrimSpace(sid)
	if f.handovers != nil {
		if err := f.handovers.Put(p); err != nil {
			return p, fmt.Errorf("compact brief persist: %w", err)
		}
	}
	if def := f.reg.Def(p.Agent); def != nil && p.CompactSessionID != "" &&
		def.SessionID == p.CompactSessionID {
		next := *def
		next.SessionID = uuid.NewString()
		p.NewSessionID = next.SessionID
		if err := f.reg.Register(next); err != nil {
			return p, fmt.Errorf("separate work session from compact: %w", err)
		}
		if f.handovers != nil {
			if err := f.handovers.Put(p); err != nil {
				return p, fmt.Errorf("persist rewritten work session: %w", err)
			}
		}
	}
	return p, nil
}

// PrepareCompaction is withdrawn (🎯T40.2). A same-provider remint is
// not how a conversation continues and not how burn is controlled.
func (f *Claudia) PrepareCompaction(name string, force bool) (handover.Pending, error) {
	return handover.Pending{}, fmt.Errorf("compact %q: withdrawn (T40.2) — same-provider remint is not a product operation", name)
}

// rotate is the shared body of provider migration. kind colours the
// errors and the log line. Compaction no longer calls this (🎯T40.2).
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
// The record is marked delivered only once the seed is confirmed to have
// reached the successor, so a failed hand-off stays pending for the next
// launch.
//
// 🎯T416 — THIS IS THE FOURTH CALLER OF THE STUCK SEND PATH, and it was the
// only one that failed CLOSED. That is not the same as being right, and an
// earlier revision of this comment said it was.
//
// WHAT WAS TRUE. The other three (deliverToSender, deliverToOverseer,
// drainAgentSendQueue) inferred success from proc.Send() returning nil and
// reported "Message sent" for a payload that never left the composer. This one
// waited for the reply, so an unsubmitted paste ran out the clock and said so
// in handOffSeed's ERROR line — emitted verbatim at 18:21 on 2026-08-10 for
// jv-t416-send-turn-begin, a true delivery failure, correctly reported, that
// nobody read. Every instrument consulted that day lied; the one that was
// honest was unconsulted.
//
// WHY THAT MADE IT LOOK CORRECT, AND WHY IT IS NOT. It did not test whether a
// turn began. It tested whether a REPLY COMPLETED inside defaultReplyTimeout
// (fleet.go, applied in awaitReply). In the born-stuck case those agree, for
// the wrong reason — a turn that never begins never completes — which is
// exactly why the arm read as truthful. In the SLOW case they part company,
// and it produced a false negative on the very worker it was seeding: the seed
// was dispatched 08:57:04Z, a human flushed the composer, it landed as a user
// message at 09:07:10Z, and this code had already logged `hand-off failed` at
// 09:07:04Z — six seconds earlier. A predicate that condemns a delivery for
// outlasting a timeout is not a delivery predicate.
//
// SO ALL FOUR CALLERS NOW LAND ON TURN-BEGIN, read from the receiver's own
// transcript (internal/turnev). Not by widening defaultReplyTimeout — clause 10
// forbids that as explicitly as it forbids widening the 45s window; the defect
// is the predicate, not the length of the clock. Deliver is still what carries
// the seed, because a bare Send breaks Grok's ACP request/response cycle, but
// its error no longer decides the verdict on a backend that keeps a transcript.
//
// What this arm still lacks is RECOVERY: "it stays pending for the next launch"
// is why that worker sat dark for 43 minutes, and a launch that may never come
// is not a retry. That half is 🎯T418 clause 5 and is deliberately not done
// here.
func (f *Claudia) SeedSuccessor(name string) (handover.Pending, bool, error) {
	if f == nil || f.handovers == nil {
		return handover.Pending{}, false, nil
	}
	pending, ok, err := f.handovers.Get(name)
	if err != nil || !ok {
		return handover.Pending{}, false, err
	}
	if pending.Remap == handover.RemapClaudiaMigrate || !pending.Usable() {
		// RemapClaudiaMigrate: Claudia already continued the destination
		// (🎯T646.1). Usable=false includes already-delivered records.
		return pending, false, nil
	}
	if pending.Seed() == "" {
		// Same-provider remint is withdrawn (🎯T40.2 / 🎯T392.1.1): a
		// compact leftover must not become a handover seed on restart.
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
//
// THE VERDICT COMES FROM THE RECEIVER, not from Deliver's error (🎯T416). The
// order matters and is the whole mechanism: snapshot the successor's transcript
// BEFORE handing the seed over, then read the region appended since, so an
// earlier copy of the same seed — a resumed migration, a re-launch — cannot
// confirm this one.
//
// The observation window is however long the delivery attempt itself took. That
// is deliberately not a new clock: one scan before, one scan after, and clause
// 10's prohibition on widening either existing clock is untouched.
func (f *Claudia) handOffSeed(name string, pending handover.Pending) {
	if pending.Remap == handover.RemapClaudiaMigrate {
		// Claudia already Send the inert continue seed. A second host
		// Deliver is the bounce-nudge bug in another costume (🎯T646.1).
		slog.Info("handover seed skipped; claudia Migrate already continued the seat", "name", name)
		return
	}
	seed := pending.Seed()
	if seed == "" {
		slog.Info("handover seed skipped; same-provider remint is withdrawn", "name", name)
		return
	}
	look := f.watchSeedArrival(name, seed)

	_, err := f.deliverSeed(name, seed)

	// A reply that came back is not the question, and a reply that timed out is
	// not the answer: ask the receiver. arrived is false on a live-stream
	// backend with no transcript to read, where Deliver's own error remains the
	// best available evidence exactly as it is on the spawn path.
	arrived, why, decidable := look()
	if decidable {
		if !arrived {
			slog.Error("handover hand-off failed; it stays pending for the next launch",
				"name", name, "err", handoffFailure(why, err))
			return
		}
	} else if err != nil {
		// 🎯T519: a live-stream successor (Codex/Grok) with a phantom Claude
		// JSONL leaves look() undecidable. "Turn already in flight" is not a
		// failed seed and must not ERROR-spam every T418 sweep — leave the
		// record pending and wait for the successor turn to end, then retry.
		if agenterr.IsPromptBusy(err) {
			slog.Info("handover seed deferred; successor turn in flight",
				"name", name, "err", err)
			return
		}
		slog.Error("handover hand-off failed; it stays pending for the next launch",
			"name", name, "err", err)
		return
	}
	if err := f.handovers.MarkDelivered(name); err != nil {
		slog.Error("handover delivered but not marked — successor may be seeded twice",
			"name", name, "err", err)
	}
	slog.Info("handover delivered", "detail", pending.Describe(), "evidence", why)
}

// handoffFailure renders why the seed is being called undelivered. It carries
// the transcript finding first, because that is what decided it, and the reply
// error only as corroboration — reversing the two is how a reply timeout came
// to be read as a delivery failure in the first place.
func handoffFailure(why string, err error) error {
	if err == nil {
		return errors.New(why)
	}
	return fmt.Errorf("%s (the delivery attempt also returned: %w)", why, err)
}

// watchSeedArrival snapshots the successor's transcript and returns a reader
// that says whether the seed reached it.
//
// decidable=false means there was nothing to read — a live-stream backend, no
// live process, or a seed too short to identify — and the caller must fall back
// rather than treat "I could not look" as "it did not arrive". That distinction
// is the same one TurnEvidence.Observed carries on the MCP side, and it exists
// because an unmeasured send reported as a defect is itself a false accusation.
//
// 🎯T519 (same surface rule as 🎯T501's liveStreamObserver): Codex and Grok
// backends advertise a Claude-shaped JSONLPath that nothing writes. A missing
// file at that path is undecidable, not "never begun". Treating it as
// born-stuck left claude→codex worker handovers pending and ERROR-spammed
// every T418 sweep with "no transcript was ever created at …jsonl".
func (f *Claudia) watchSeedArrival(name, seed string) func() (arrived bool, why string, decidable bool) {
	undecidable := func() (bool, string, bool) { return false, "", false }
	if f == nil {
		return undecidable
	}
	path := strings.TrimSpace(f.successorTranscript(name))
	needle := turnev.Needle(seed)
	if path == "" || needle == "" {
		return undecidable
	}
	keepsClaude := f.successorKeepsClaudeTranscript(name)
	baseline, had := turnev.Size(path)
	return func() (bool, string, bool) {
		fate := turnev.Scan(path, baseline, had, needle)
		if fate.Delivered() {
			return true, fmt.Sprintf("the successor's transcript %s carries the seed (%s)", path, fate), true
		}
		if fate == turnev.FateQueued {
			// The successor has it and is mid-turn. Marking it delivered is
			// right: the record exists to stop a successor coming up cold, and
			// a seed sitting in the receiver's own queue will be drained by it.
			return true, fmt.Sprintf("the successor's transcript %s shows the seed enqueued behind a live turn", path), true
		}
		if turnev.Missing(path) {
			if !keepsClaude {
				return false, "", false
			}
			return false, fmt.Sprintf("no transcript was ever created at %s — the successor has never begun a turn", path), true
		}
		return false, fmt.Sprintf("transcript %s never gained the seed", path), true
	}
}

// successorKeepsClaudeTranscript reports whether the successor's backend
// actually maintains the durable ~/.claude/projects JSONL. Same rule as
// mcpserver.providerKeepsClaudeTranscript (🎯T501 / 🎯T519).
func (f *Claudia) successorKeepsClaudeTranscript(name string) bool {
	if f == nil || f.reg == nil {
		return true
	}
	def := f.reg.Def(name)
	if def == nil {
		return true
	}
	return providerKeepsClaudeTranscript(def.Provider)
}

// providerKeepsClaudeTranscript mirrors the T501 mint-path classifier: only
// Claude-shaped agents keep a durable Claude JSONL; Codex and Grok are
// live-stream surfaces.
func providerKeepsClaudeTranscript(p claudia.Provider) bool {
	return p == "" || p == claudia.ProviderClaude
}

// successorTranscript is where the successor records what it was told: the
// live process's JSONL on the product path, overridable for the oracle.
func (f *Claudia) successorTranscript(name string) string {
	if f.seedTranscript != nil {
		return f.seedTranscript(name)
	}
	if f.reg == nil {
		return ""
	}
	ag := f.reg.Get(name)
	if ag == nil {
		return ""
	}
	return ag.JSONLPath()
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

func (f *Claudia) trySelfBrief(p handover.Pending) (string, error) {
	if f.selfBrief != nil {
		return f.selfBrief(p)
	}
	ag := f.reg.Get(p.Agent)
	if ag == nil || !ag.Alive() {
		return "", errOutgoingDead
	}
	// Bounded: a live outgoing that is busy or slow must not stall the
	// switch. Timeout falls through to Distill (🎯T285.1).
	type reply struct {
		text string
		err  error
	}
	ch := make(chan reply, 1)
	go func() {
		text, err := f.Deliver(p.Agent, selfBriefPrompt)
		ch <- reply{text, err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			return "", got.err
		}
		return strings.TrimSpace(got.text), nil
	case <-time.After(15 * time.Second):
		return "", fmt.Errorf("self-brief timeout")
	}
}

func (f *Claudia) runThrowawayCompact(p handover.Pending) (string, string, error) {
	if f.compactBrief != nil {
		return f.compactBrief(p)
	}
	return f.launchThrowawayCompact(p)
}

const selfBriefPrompt = "Write a short brief of in-flight work only (promises, open threads, last decision). No preamble. Do not continue the work."

var errOutgoingDead = errors.New("outgoing session is not live")

// launchThrowawayCompact starts a throwaway session on the NEW provider
// whose only job is to read the predecessor file. The work session is a
// later Start on a different session_id.
func (f *Claudia) launchThrowawayCompact(p handover.Pending) (string, string, error) {
	if f == nil || f.reg == nil {
		return "", "", fmt.Errorf("throwaway compact: no registry")
	}
	def := f.reg.Def(p.Agent)
	if def == nil {
		return "", "", fmt.Errorf("throwaway compact: no agent %q", p.Agent)
	}
	sid := uuid.NewString()
	temp := "jv-compact-" + sid[:8]
	tempDef, err := throwawayCompactDef(*def, temp, sid, claudia.Provider(p.To))
	if err != nil {
		return "", "", fmt.Errorf("throwaway compact: %w", err)
	}
	if err := f.reg.Register(tempDef); err != nil {
		return "", "", err
	}
	defer func() {
		f.reg.Stop(temp)
		_, _ = f.removals.Remove(f.reg, temp, fleetlog.Removal{
			Reason: fleetlog.ReasonRotationDrop,
			Detail: "throwaway compact session",
		})
	}()
	if err := f.Launch(&thread.Thread{ID: temp}); err != nil {
		return "", "", err
	}
	prompt := fmt.Sprintf(
		"This is a throwaway compact session. Read the predecessor transcript at %s and reply with a short brief of in-flight work only. Do not continue the work.",
		p.TranscriptPath)
	text, err := f.Deliver(temp, prompt)
	if err != nil {
		return sid, "", err
	}
	return sid, strings.TrimSpace(text), nil
}

var errNoLiveAgent = errors.New("migrate: no live session to remap")

// remapViaClaudia is the 🎯T622 live path: Claudia owns the session switch
// and inert handover. The returned Pending is a result for existing host
// callers, not a second durable handover. ok is false only when no live
// session can perform the move and the caller must use the cold fallback.
func (f *Claudia) remapViaClaudia(name string, target claudia.Provider, model string, force bool, draft handover.Pending) (handover.Pending, bool, error) {
	if f == nil || f.reg == nil {
		return handover.Pending{}, false, nil
	}
	if f.liveMigrate == nil && f.reg.Get(name) == nil {
		return handover.Pending{}, false, nil
	}
	sourceDef := f.reg.Def(name)
	if sourceDef == nil {
		return handover.Pending{}, true, fmt.Errorf("migrate %q: registry row vanished before Agent.Migrate", name)
	}
	if err := providerSwitchRefusal(*sourceDef, target); err != nil {
		return handover.Pending{}, true, fmt.Errorf("migrate %q: %w", name, err)
	}
	if f.handovers != nil {
		// An old Jevons handover must be removed before the live Claudia
		// operation starts. Refuse before moving if that cleanup fails.
		if err := f.handovers.Clear(name); err != nil {
			return handover.Pending{}, true, fmt.Errorf("migrate %q: clear obsolete host handover: %w", name, err)
		}
	}
	var retained string
	if draft.Brief == "" && f.retainedHistory != nil {
		var err error
		retained, err = f.retainedHistory(name)
		if err != nil {
			return handover.Pending{}, true, fmt.Errorf("migrate %q: retained predecessor context: %w", name, err)
		}
	}
	args := &MigrateRequest{
		Provider: target, Model: model, Force: force, Reason: "explicit",
		ContextBrief: draft.Brief, RetainedTranscript: retained,
	}
	err := f.invokeMigrate(name, args)
	if force && err != nil && strings.Contains(err.Error(), "turn in flight") {
		// A seat that is reported to every minute has no gap between turns:
		// jevons-po and the overseer each refused a forced migrate for as long
		// as anyone kept asking on 2026-09-22. Force is the caller saying the
		// move outranks the turn, so end the turn and ask once more.
		slog.Warn("forced migrate found a turn in flight; interrupting it", "name", name, "to", target)
		if live := f.reg.Get(name); live != nil && live.Alive() {
			if ierr := live.Interrupt(); ierr != nil {
				slog.Warn("interrupt before forced migrate failed", "name", name, "err", ierr)
			}
		}
		time.Sleep(migrateInterruptSettle)
		err = f.invokeMigrate(name, args)
	}
	if err != nil {
		if isLiveMigrateFallback(err) {
			return handover.Pending{}, false, nil
		}
		if !alreadyMigratedTo(err, target) {
			return handover.Pending{}, true, err
		}
		// The registry row still names the old provider (the refusal above
		// would have fired otherwise) while the live agent is already on the
		// target: an earlier Migrate landed in claudia and was never recorded
		// here. Record it now, so a retry converges.
		slog.Warn("live agent was already on the target provider; recording the migration the registry missed",
			"name", name, "to", target)
	}
	def := f.reg.Def(name)
	if def == nil {
		return handover.Pending{}, true, fmt.Errorf("migrate %q: registry row vanished after Agent.Migrate", name)
	}
	fromModel := sourceDef.Model
	fromProvider := string(sourceDef.Provider)
	next := *def
	// A registered Claudia Agent.Migrate has already committed the provider,
	// model and real destination session id. Re-registering that row from a
	// host reconstruction can replace the real id with an invented UUID.
	claudiaRecorded := cli.PlanProvider(def.Provider) == cli.PlanProvider(target) &&
		def.SessionID != "" && def.SessionID != sourceDef.SessionID
	if !claudiaRecorded {
		if err := switchProvider(&next, target, "migrate"); err != nil {
			return handover.Pending{}, true, fmt.Errorf("migrate %q: %w", name, err)
		}
		next.ConnectURL = ""
		next.ConnectPID = 0
		if model != "" {
			next.Model = cli.BindSessionModel(model, target)
		} else if target != def.Provider {
			next.Model = cli.BindSessionModel("", target)
		}
	}
	// 🎯T790: Materialized promises that SessionID names a session that
	// exists. Only a session id read from the live agent keeps that promise;
	// a fallback uuid was never written anywhere, and recording it as
	// Materialized makes the next Launch insist on resuming a conversation
	// that is not there (jevons got 227ea2e6 and jevons-po 60c71bb8 on
	// 2026-09-22). With no readable id the row is a fresh mint instead.
	nextSession, liveModel := f.liveSessionOf(name)
	sessionRead := nextSession != "" && nextSession != sourceDef.SessionID
	if claudiaRecorded {
		if nextSession != "" && nextSession != def.SessionID {
			return handover.Pending{}, true, fmt.Errorf("migrate %q: Claudia recorded session %s but live agent reports %s", name, def.SessionID, nextSession)
		}
		nextSession = def.SessionID
		sessionRead = true
	} else if !sessionRead {
		nextSession = uuid.NewString()
		slog.Warn("migrate: live session id unreadable; recording the row as a fresh mint, not Materialized",
			"name", name, "to", target)
	}
	// The id the live agent still reports is the new version only when it
	// is not the one this change left. A provider that does not report a
	// model keeps answering with the previous id; writing that back is
	// the version surviving the change. A different id is the model the
	// successor is actually on.
	if !claudiaRecorded && liveModel != "" && model == "" && liveModel != strings.TrimSpace(fromModel) {
		next.Model = liveModel
	}
	if !claudiaRecorded {
		next.SessionID = nextSession
		next.Materialized = sessionRead
		if err := f.reg.Register(next); err != nil {
			return handover.Pending{}, true, fmt.Errorf("migrate %q: record remapped row: %w", name, err)
		}
	}
	f.noteModelSwitch(&ModelSwitch{
		Name:         name,
		Provider:     string(next.Provider),
		FromProvider: fromProvider,
		From:         fromModel,
		To:           next.Model,
		How:          ModelSwitchHowMigrate,
	})
	pending := draft
	pending.To = string(target)
	pending.NewSessionID = nextSession
	pending.SessionUnread = !sessionRead
	pending.Remap = handover.RemapClaudiaMigrate
	pending.Delivered = true // Claudia seeded the successor; Jevons must not.
	pending.Purpose = next.Purpose
	pending.WorkDir = next.WorkDir
	pending.Parent = next.Parent
	pending.Model = next.Model
	pending.TargetID = next.TargetID
	pending.Goal = next.Goal
	if f.rotations != nil {
		_ = f.rotations.Put(handover.Rotation{Agent: name, Kind: "migrate"})
	}
	slog.Info("agent session remapped via claudia Migrate",
		"name", name, "from", pending.From, "to", pending.To,
		"old_session", pending.OldSessionID, "new_session", nextSession)
	return pending, true, nil
}

// liveSessionOf reads the session id and model of the live agent behind
// name ("" when there is none or it cannot be read).
func (f *Claudia) liveSessionOf(name string) (sessionID, model string) {
	if f.liveSession != nil {
		return f.liveSession(name)
	}
	if live := f.reg.Get(name); live != nil {
		return strings.TrimSpace(live.SessionID()), strings.TrimSpace(live.Model())
	}
	return "", ""
}

func (f *Claudia) invokeMigrate(name string, args *MigrateRequest) error {
	if f.liveMigrate != nil {
		return f.liveMigrate(args)
	}
	live := f.reg.Get(name)
	if live == nil {
		return errNoLiveAgent
	}
	return live.Migrate(args.migrateArgs())
}

// migrateInterruptSettle is how long a forced migrate waits after
// interrupting a seat before it asks claudia again.
var migrateInterruptSettle = 3 * time.Second

// alreadyMigratedTo reports claudia's refusal to migrate an agent onto the
// provider it is already on.
//
// On 2026-09-21 an overseer migrate call outlived its HTTP client: claudia
// finished moving the agent to Cursor, the caller had gone, and nothing wrote
// the registry row. Every retry was then refused with "same provider cursor"
// while the row went on saying codex — so the next daemon bounce would have
// relaunched the overseer on the provider it had just been moved off.
func alreadyMigratedTo(err error, target claudia.Provider) bool {
	return err != nil && (strings.Contains(err.Error(), "Migrate: same provider "+string(target)) ||
		strings.Contains(err.Error(), "Migrate: same provider "+string(cli.PlanProvider(target))))
}

func isLiveMigrateFallback(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errNoLiveAgent) {
		return true
	}
	// Claudia's disposable transfer seat can fail with the same process
	// wording. A nested transfer failure says nothing about the source seat;
	// treating it as dead invokes MigrateStopped while its live handle still
	// exists and hides the real error behind "live handle exists".
	msg := strings.TrimSpace(err.Error())
	return strings.HasPrefix(msg, "agent process not running") ||
		strings.HasPrefix(msg, "agent not ready")
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

func throwawayCompactDef(source claudia.AgentDef, name, sessionID string, provider claudia.Provider) (claudia.AgentDef, error) {
	source.Name = name
	source.SessionID = sessionID
	if err := switchProvider(&source, provider, "throwaway compact"); err != nil {
		return claudia.AgentDef{}, err
	}
	// This row exists only long enough to ask for one compact brief. Keep it
	// outside every work-seat policy (plan migration, recovery, idle nudges)
	// and never make it look engaged on the predecessor's target (🎯T543).
	source.Purpose = claudia.PurposeAside
	source.TargetID = ""
	source.Materialized = false
	source.AutoStart = false
	source.ConnectURL = ""
	source.ConnectPID = 0
	return source, nil
}
