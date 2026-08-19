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
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/turnev"
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

	// Gather the work-session brief BEFORE rotate stops the outgoing
	// process. Live self-brief needs that process; Distill and the
	// throwaway compact do not. Rotate then mints the WORK session
	// — a different id from any compact session (🎯T285.1).
	oldSession := def.SessionID
	transcript := discovery.TranscriptPath(f.roots, oldSession)
	if transcript == "" && !force {
		return handover.Pending{}, fmt.Errorf(
			"migrate %q: no transcript found for session %s under the configured session roots — "+
				"its history cannot be handed over; pass force to switch cold anyway",
			name, oldSession)
	}
	draft := handover.Pending{
		Agent:          name,
		From:           string(def.Provider),
		To:             string(target),
		Kind:           handover.KindMigrate,
		OldSessionID:   oldSession,
		TranscriptPath: transcript,
	}
	brief := handover.GatherBrief(draft, handover.GatherHooks{
		SelfBrief: f.trySelfBrief,
		// Compact is the test hook only. The product throwaway session
		// is launched from CompleteThinBrief after PrepareMigration, so
		// hermetic rotate tests never block on a live provider.
		Compact: f.compactBrief,
	})
	draft.Brief = brief.Text
	draft.BriefSource = string(brief.Source)
	draft.CompactSessionID = brief.CompactSessionID

	pending, err := f.rotate(name, target, force, "migrate")
	if err != nil {
		return pending, err
	}
	pending.Brief = draft.Brief
	pending.BriefSource = draft.BriefSource
	pending.CompactSessionID = draft.CompactSessionID
	if f.handovers != nil {
		if err := f.handovers.Put(pending); err != nil {
			return pending, fmt.Errorf("migrate %q: persist brief: %w", name, err)
		}
	}
	if def := f.reg.Def(name); def != nil && pending.CompactSessionID != "" &&
		def.SessionID == pending.CompactSessionID {
		next := *def
		next.SessionID = uuid.NewString()
		pending.NewSessionID = next.SessionID
		if err := f.reg.Register(next); err != nil {
			return pending, fmt.Errorf("migrate %q: separate work session from compact: %w", name, err)
		}
		if f.handovers != nil {
			if err := f.handovers.Put(pending); err != nil {
				return pending, fmt.Errorf("migrate %q: persist rewritten work session: %w", name, err)
			}
		}
	}
	return pending, nil
}

// CompleteThinBrief runs the throwaway compact session when Distill
// extracted nothing useful. Failure falls through — the switch must not
// stall. The work session id on the registry row is kept distinct from
// the compact session (🎯T285.1).
func (f *Claudia) CompleteThinBrief(p handover.Pending) (handover.Pending, error) {
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

	next := *def
	next.Provider = target
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
	seed := pending.Seed()
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
	tempDef := *def
	tempDef.Name = temp
	tempDef.SessionID = sid
	tempDef.Provider = claudia.Provider(p.To)
	tempDef.Materialized = false
	tempDef.AutoStart = false
	tempDef.ConnectURL = ""
	tempDef.ConnectPID = 0
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
