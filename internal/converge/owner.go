// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// Owner-interaction convergence (🎯T355).
//
// The fleet half of this package (gap.go / set.go) keeps one desired state:
// an open mission is being worked. This file keeps the *owner-facing* one:
// talking to Jevons works. They are deliberately domain-parallel — the fleet
// MissionGap schema is not overloaded with chat fields — but they share one
// doctrine and one vocabulary:
//
//	observation  what the owner-facing plant looks like right now
//	step         an action taken toward closing the gap
//	satisfaction the desired state actually holding
//
// Only an observation satisfies. Publishing an idle level sample, requeueing
// an owner turn, or interrupting a stalled prompt are steps: they record
// progress and nothing more. The next observation decides.
//
// Three dimensions carry the owner-visible contract:
//
//	send-landed        the owner's turn is durable (journal) and acked (queue
//	                   drained to the overseer) — never a ghost send
//	chrome-truthful    the working indicator the owner sees matches server
//	                   level truth — no eternal spinner, no silent work
//	reply-or-residual  a sealed reply, or a *named* residual, within a bound —
//	                   silence is never an outcome
//
// A fourth, interaction-usable, covers the client itself (composer blocked,
// main-thread stalled) so a frozen tab is an observed outage class rather
// than an owner complaint.
//
// Daemon restart (🎯T171) is one outage class in this model, not the model:
// after a bounce an unanswered owner turn is exactly reply-or-residual in the
// owner_turn_stall kind, and its actuator — re-inject the unacked owner text —
// is owner-intent-resume (🎯T328) generalized. ACP stall, delivery failure and
// UX degrade enter the same loop through their own kinds.

// OwnerDimension is one axis of owner-interaction health. Each dimension
// holds at most one standing gap, so a lying spinner and an undelivered send
// are tracked (and stepped) separately.
type OwnerDimension string

const (
	// OwnerDimSendLanded: the newest owner turn is durable and acked.
	OwnerDimSendLanded OwnerDimension = "send_landed"
	// OwnerDimChromeTruthful: published working chrome matches level truth.
	OwnerDimChromeTruthful OwnerDimension = "chrome_truthful"
	// OwnerDimReplyOrResidual: the newest owner turn ended in a sealed reply
	// or a named residual.
	OwnerDimReplyOrResidual OwnerDimension = "reply_or_residual"
	// OwnerDimInteractive: the owner can actually drive the client.
	OwnerDimInteractive OwnerDimension = "interaction_usable"
)

// OwnerDimensions is every dimension, in classification order.
var OwnerDimensions = []OwnerDimension{
	OwnerDimSendLanded,
	OwnerDimChromeTruthful,
	OwnerDimReplyOrResidual,
	OwnerDimInteractive,
}

// OwnerGapKind is the outage class behind a standing owner gap. It is what
// routes the actuator, and it is the generalization of "daemon restarted":
// restart is one way to arrive at owner_turn_stall, not a class of its own.
type OwnerGapKind string

const (
	// OwnerGapSendNotLanded: the owner's text never reached the durable
	// chatlog — the silent-delivery failure class (🎯T305).
	OwnerGapSendNotLanded OwnerGapKind = "send_not_landed"
	// OwnerGapSendNotDelivered: journaled but never acked to the overseer;
	// the turn is sitting in the notify queue or was dropped on the floor.
	OwnerGapSendNotDelivered OwnerGapKind = "send_not_delivered"
	// OwnerGapFalseWorking: the owner sees a spinner while nothing is in
	// flight — the lying-chrome class this target was raised for.
	OwnerGapFalseWorking OwnerGapKind = "chrome_false_working"
	// OwnerGapFalseIdle: a turn is in flight while the chrome says idle.
	OwnerGapFalseIdle OwnerGapKind = "chrome_false_idle"
	// OwnerGapACPStall: the prompt is genuinely in flight but the provider
	// has produced no progress for longer than the stall bound.
	OwnerGapACPStall OwnerGapKind = "acp_stall"
	// OwnerGapTurnStall: no reply, no residual, and nothing in flight —
	// the turn evaporated (restart, dropped queue, lost session).
	OwnerGapTurnStall OwnerGapKind = "owner_turn_stall"
	// OwnerGapUXDegraded: the client cannot be driven (composer blocked or
	// the main thread stopped ticking).
	OwnerGapUXDegraded OwnerGapKind = "ux_degraded"
)

// OwnerBounds are the documented tolerances of the contract. They are the
// "within a documented bound" half of reply-or-residual, and they double as
// the minimum spacing between two steps on the same dimension.
type OwnerBounds struct {
	// SendLand: how long a submitted turn may take to become durable+acked.
	SendLand time.Duration
	// ChromeTruth: how long chrome may disagree with level truth before it
	// counts as lying (covers the normal send→drain race).
	ChromeTruth time.Duration
	// Reply: how long an owner turn may sit with no reply, no residual and
	// nothing in flight.
	Reply time.Duration
	// ACPStall: how long an in-flight prompt may produce no progress.
	// Deliberately longer than the cockpit's own stuck-busy timeout
	// (🎯T204) so the generic recovery acts first and this is the backstop.
	ACPStall time.Duration
	// UIHeartbeat: how stale a client heartbeat may get while connected.
	UIHeartbeat time.Duration
}

// DefaultOwnerBounds is the shipped contract.
func DefaultOwnerBounds() OwnerBounds {
	return OwnerBounds{
		SendLand:    5 * time.Second,
		ChromeTruth: 15 * time.Second,
		Reply:       90 * time.Second,
		ACPStall:    2 * time.Minute,
		UIHeartbeat: 90 * time.Second,
	}
}

func (b OwnerBounds) withDefaults() OwnerBounds {
	d := DefaultOwnerBounds()
	if b.SendLand <= 0 {
		b.SendLand = d.SendLand
	}
	if b.ChromeTruth <= 0 {
		b.ChromeTruth = d.ChromeTruth
	}
	if b.Reply <= 0 {
		b.Reply = d.Reply
	}
	if b.ACPStall <= 0 {
		b.ACPStall = d.ACPStall
	}
	if b.UIHeartbeat <= 0 {
		b.UIHeartbeat = d.UIHeartbeat
	}
	return b
}

// For returns the step spacing that applies to one gap kind.
func (b OwnerBounds) For(kind OwnerGapKind) time.Duration {
	b = b.withDefaults()
	switch kind {
	case OwnerGapSendNotLanded, OwnerGapSendNotDelivered:
		return b.SendLand
	case OwnerGapFalseWorking, OwnerGapFalseIdle:
		return b.ChromeTruth
	case OwnerGapACPStall:
		return b.ACPStall
	case OwnerGapUXDegraded:
		return b.UIHeartbeat
	default:
		return b.Reply
	}
}

// OwnerObservation is a pure snapshot of the owner-facing plant. The caller
// fills it from the server (chat listeners, chatlog, notify queue, ACP
// progress) and passes `now` explicitly; this package does no I/O.
type OwnerObservation struct {
	// ClientsConnected is the number of live owner chat connections. Zero
	// scopes out the dimensions only a watching owner can experience.
	ClientsConnected int

	// --- send-landed ---

	// SendID identifies the newest owner submission ("" = none seen).
	SendID string
	// SendAt is when the client's text was accepted on the wire.
	SendAt time.Time
	// SendJournaled is true once that text is durable in the chatlog.
	SendJournaled bool
	// SendDelivered is true once the overseer actually took the prompt
	// (queue drained), i.e. the server ack half of send-landed.
	SendDelivered bool

	// --- chrome-truthful ---

	// ChromeWorking is the working level the clients were last told to show.
	ChromeWorking bool
	// TurnInFlight is server level truth: an owner-visible turn is running.
	TurnInFlight bool
	// ChromeMismatchSince is when the current disagreement began (zero when
	// chrome and truth agree, or when the caller does not track it — which
	// is read as "already past grace").
	ChromeMismatchSince time.Time

	// --- reply-or-residual ---

	// OwnerTurnAt is when the newest owner turn opened ("" = none).
	OwnerTurnAt time.Time
	// ReplySealedAt is when an assistant reply sealed for that turn.
	ReplySealedAt time.Time
	// Residual names why no reply is owed (cancelled_by_owner,
	// delivery_failed, overseer_restarted, …); ResidualAt is when it was
	// recorded. A residual only counts when it lands after the turn.
	Residual   string
	ResidualAt time.Time
	// PromptInFlight is provider-level truth from the overseer handle.
	PromptInFlight bool
	// QueueDepth is owner/notify text still waiting to be delivered.
	QueueDepth int
	// SinceACPProgress is time since the last provider event or successful
	// send (zero when progress was just seen).
	SinceACPProgress time.Duration

	// --- interaction-usable ---

	// SinceUIHeartbeat is how stale the newest client heartbeat is. Zero
	// means fresh, or that no client heartbeat is tracked at all.
	SinceUIHeartbeat time.Duration
	// ComposerBlocked is a client-reported inability to submit.
	ComposerBlocked bool
}

// OwnerChromeMismatch reports whether published chrome disagrees with level
// truth. Callers use it to maintain ChromeMismatchSince so the predicate has
// exactly one definition.
func OwnerChromeMismatch(o OwnerObservation) bool { return o.ChromeWorking != o.TurnInFlight }

// OwnerVerdict is one dimension's classification of one observation.
type OwnerVerdict struct {
	Dimension OwnerDimension
	Condition Condition
	Kind      OwnerGapKind
	Reason    string
}

// ClassifyOwnerInteraction is the decidable core: one observation in, one
// verdict per dimension out, in OwnerDimensions order. It never consults step
// history — no number of requeues or idle samples can change the answer.
func ClassifyOwnerInteraction(o OwnerObservation, now time.Time, b OwnerBounds) []OwnerVerdict {
	b = b.withDefaults()
	return []OwnerVerdict{
		classifyOwnerSend(o, now, b),
		classifyOwnerChrome(o, now, b),
		classifyOwnerReply(o, now, b),
		classifyOwnerInteractive(o, b),
	}
}

func classifyOwnerSend(o OwnerObservation, now time.Time, b OwnerBounds) OwnerVerdict {
	v := OwnerVerdict{Dimension: OwnerDimSendLanded}
	switch {
	case o.SendAt.IsZero():
		v.Condition, v.Reason = ConditionOutOfScope, "no_owner_send"
	case o.SendJournaled && o.SendDelivered:
		v.Condition, v.Reason = ConditionSatisfied, "send_landed"
	case now.Sub(o.SendAt) < b.SendLand:
		v.Condition, v.Reason = ConditionSatisfied, "within_land_bound"
	case !o.SendJournaled:
		v.Condition, v.Kind, v.Reason = ConditionGap, OwnerGapSendNotLanded, "not_durable_in_chatlog"
	default:
		v.Condition, v.Kind, v.Reason = ConditionGap, OwnerGapSendNotDelivered, "not_acked_by_overseer"
	}
	return v
}

func classifyOwnerChrome(o OwnerObservation, now time.Time, b OwnerBounds) OwnerVerdict {
	v := OwnerVerdict{Dimension: OwnerDimChromeTruthful}
	if o.ClientsConnected <= 0 {
		v.Condition, v.Reason = ConditionOutOfScope, "no_client_connected"
		return v
	}
	if !OwnerChromeMismatch(o) {
		v.Condition, v.Reason = ConditionSatisfied, "chrome_matches_level"
		return v
	}
	// A disagreement younger than the grace is the ordinary send→drain race.
	if !o.ChromeMismatchSince.IsZero() && now.Sub(o.ChromeMismatchSince) < b.ChromeTruth {
		v.Condition, v.Reason = ConditionSatisfied, "mismatch_within_grace"
		return v
	}
	v.Condition = ConditionGap
	if o.ChromeWorking {
		v.Kind, v.Reason = OwnerGapFalseWorking, "working_chrome_without_turn"
	} else {
		v.Kind, v.Reason = OwnerGapFalseIdle, "turn_in_flight_without_chrome"
	}
	return v
}

func classifyOwnerReply(o OwnerObservation, now time.Time, b OwnerBounds) OwnerVerdict {
	v := OwnerVerdict{Dimension: OwnerDimReplyOrResidual}
	switch {
	case o.OwnerTurnAt.IsZero():
		v.Condition, v.Reason = ConditionOutOfScope, "no_owner_turn"
	case o.ReplySealedAt.After(o.OwnerTurnAt):
		v.Condition, v.Reason = ConditionSatisfied, "reply_sealed"
	case strings.TrimSpace(o.Residual) != "" && o.ResidualAt.After(o.OwnerTurnAt):
		v.Condition, v.Reason = ConditionSatisfied, "residual:"+strings.TrimSpace(o.Residual)
	case o.PromptInFlight && o.SinceACPProgress < b.ACPStall:
		// A long turn that is still producing events is healthy: the bound
		// governs silence, not duration.
		v.Condition, v.Reason = ConditionSatisfied, "turn_progressing"
	case o.PromptInFlight:
		v.Condition, v.Kind, v.Reason = ConditionGap, OwnerGapACPStall, "prompt_in_flight_no_progress"
	case now.Sub(o.OwnerTurnAt) < b.Reply:
		v.Condition, v.Reason = ConditionSatisfied, "within_reply_bound"
	case o.QueueDepth > 0:
		v.Condition, v.Kind, v.Reason = ConditionGap, OwnerGapTurnStall, "queued_undelivered"
	default:
		v.Condition, v.Kind, v.Reason = ConditionGap, OwnerGapTurnStall, "no_reply_no_residual"
	}
	return v
}

func classifyOwnerInteractive(o OwnerObservation, b OwnerBounds) OwnerVerdict {
	v := OwnerVerdict{Dimension: OwnerDimInteractive}
	switch {
	case o.ClientsConnected <= 0:
		v.Condition, v.Reason = ConditionOutOfScope, "no_client_connected"
	case o.ComposerBlocked:
		v.Condition, v.Kind, v.Reason = ConditionGap, OwnerGapUXDegraded, "composer_blocked"
	case o.SinceUIHeartbeat >= b.UIHeartbeat:
		v.Condition, v.Kind, v.Reason = ConditionGap, OwnerGapUXDegraded, "ui_heartbeat_stale"
	default:
		v.Condition, v.Reason = ConditionSatisfied, "interaction_usable"
	}
	return v
}

// Owner-domain step kinds. They join the fleet kinds in one vocabulary; the
// escalation rungs (StepOverseerNoise, StepHumanAlert) are shared.
const (
	// StepRequeueOwnerSend re-injects the unacked owner text. This is
	// owner-intent-resume (🎯T328) generalized past restart.
	StepRequeueOwnerSend StepKind = "requeue_owner_send"
	// StepPublishLevelTruth re-publishes the server's working level so the
	// chrome stops lying in either direction.
	StepPublishLevelTruth StepKind = "publish_level_truth"
	// StepACPUnstick interrupts a prompt that really is in flight. The kind
	// gates it: a stalled *queue* is never unstuck by an interrupt.
	StepACPUnstick StepKind = "acp_unstick"
	// StepUXCoordinate asks the client to yield and re-hydrate (🎯T349) and
	// settles chrome so a recovered tab is not left spinning.
	StepUXCoordinate StepKind = "ux_coordinate"
)

// OwnerMaxPrimarySteps is how many times the kind's own actuator is tried
// before the gap escalates to noise. The actuator is not the point — an
// unsatisfied gap after this many attempts is a report, not a retry.
const OwnerMaxPrimarySteps = 3

// OwnerPrimaryStep is the actuator that directly addresses one gap kind.
func OwnerPrimaryStep(kind OwnerGapKind) StepKind {
	switch kind {
	case OwnerGapSendNotLanded, OwnerGapSendNotDelivered, OwnerGapTurnStall:
		return StepRequeueOwnerSend
	case OwnerGapFalseWorking, OwnerGapFalseIdle:
		return StepPublishLevelTruth
	case OwnerGapACPStall:
		return StepACPUnstick
	case OwnerGapUXDegraded:
		return StepUXCoordinate
	}
	return ""
}

// PlanOwnerStep chooses the next step for a standing gap: the kind's own
// actuator while attempts remain, then one overseer-level report, then the
// owner-visible alert. Planning a step is not satisfaction — the gap stays
// standing until an observation says otherwise.
func PlanOwnerStep(g OwnerGap) StepKind {
	primary := OwnerPrimaryStep(g.Kind)
	if primary == "" {
		return ""
	}
	if g.StepsByKind[primary] < OwnerMaxPrimarySteps {
		return primary
	}
	if g.StepsByKind[StepOverseerNoise] < 1 {
		return StepOverseerNoise
	}
	return StepHumanAlert
}

// OwnerGap is a standing owner-interaction gap: one dimension whose desired
// state is not holding. It is the owner-domain sibling of MissionGap, not a
// reuse of it — the fields that matter here are a dimension and an outage
// class, not an agent and a mission.
type OwnerGap struct {
	Dimension OwnerDimension
	Kind      OwnerGapKind
	Reason    string
	OpenedAt  time.Time
	// LastSeen is the most recent observation that kept this gap open.
	LastSeen time.Time
	// Episode counts how many times this dimension has entered a gap,
	// including this one; it survives satisfaction so a flapping chrome is
	// visible as flapping.
	Episode int

	// Steps taken during this episode, oldest first.
	Steps []StepRecord
	// StepsByKind counts step attempts of each kind this episode.
	StepsByKind map[StepKind]int
	// LastStepAt is the most recent step attempt (zero = none).
	LastStepAt time.Time
}

// Key is the gap's identity in the shared StepRecord vocabulary.
func (g OwnerGap) Key() GapKey { return GapKey("owner:" + string(g.Dimension)) }

// Age is how long this gap has been open at now.
func (g OwnerGap) Age(now time.Time) time.Duration {
	if g.OpenedAt.IsZero() {
		return 0
	}
	return now.Sub(g.OpenedAt)
}

// SinceLastStep is how long since the last step attempt; a never-stepped gap
// reports its full age so it is immediately actionable.
func (g OwnerGap) SinceLastStep(now time.Time) time.Duration {
	if g.LastStepAt.IsZero() {
		return g.Age(now)
	}
	return now.Sub(g.LastStepAt)
}

// StepCount is the total number of step attempts this episode.
func (g OwnerGap) StepCount() int { return len(g.Steps) }

// Satisfied is always false, for the same reason MissionGap.Satisfied is: a
// gap in the set is unsatisfied by construction.
func (g OwnerGap) Satisfied() bool { return false }

// LadderView projects the gap into the 🎯T317 escalation ladder's type, so
// owner-interaction gaps can ride the same rungs as fleet mission gaps.
func (g OwnerGap) LadderView() Gap {
	return Gap{Agent: "owner", Mission: string(g.Dimension), Since: g.OpenedAt}
}

func (g OwnerGap) clone() OwnerGap {
	out := g
	if len(g.Steps) > 0 {
		out.Steps = append([]StepRecord(nil), g.Steps...)
	}
	if len(g.StepsByKind) > 0 {
		out.StepsByKind = make(map[StepKind]int, len(g.StepsByKind))
		for k, v := range g.StepsByKind {
			out.StepsByKind[k] = v
		}
	}
	return out
}

// OwnerOutcome is the result of reconciling one verdict against the set.
type OwnerOutcome struct {
	Dimension  OwnerDimension
	Resolution Resolution
	Condition  Condition
	Reason     string
	// Gap is the gap as it now stands (zero-valued when it left the set).
	Gap OwnerGap
}

// OwnerSet is the standing set of owner-interaction gaps, at most one per
// dimension. Same contract as the fleet Set: Reconcile is the only method
// that can remove a gap, and it removes one only on an observation that
// classifies as satisfied or out-of-scope.
type OwnerSet struct {
	mu       sync.Mutex
	gaps     map[OwnerDimension]*OwnerGap
	episodes map[OwnerDimension]int
}

// NewOwnerSet returns an empty owner-interaction reconcile set.
func NewOwnerSet() *OwnerSet {
	return &OwnerSet{gaps: map[OwnerDimension]*OwnerGap{}, episodes: map[OwnerDimension]int{}}
}

// Reconcile folds one verdict into the set and reports what happened.
func (s *OwnerSet) Reconcile(v OwnerVerdict, now time.Time) OwnerOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.Dimension == "" {
		return OwnerOutcome{Resolution: ResolutionNone, Condition: v.Condition, Reason: "empty_dimension"}
	}
	existing := s.gaps[v.Dimension]
	out := OwnerOutcome{
		Dimension:  v.Dimension,
		Condition:  v.Condition,
		Reason:     v.Reason,
		Resolution: ResolutionNone,
	}

	switch v.Condition {
	case ConditionSatisfied, ConditionOutOfScope:
		if existing != nil {
			delete(s.gaps, v.Dimension)
			out.Gap = existing.clone()
			if v.Condition == ConditionSatisfied {
				out.Resolution = ResolutionSatisfied
			} else {
				out.Resolution = ResolutionWithdrawn
			}
		}
		return out
	}

	// ConditionGap: open, or keep open. A change of kind within the same
	// dimension is the same episode with a new classification — the owner
	// is still not being served.
	if existing != nil {
		existing.Kind = v.Kind
		existing.Reason = v.Reason
		existing.LastSeen = now
		out.Resolution = ResolutionPersisting
		out.Gap = existing.clone()
		return out
	}
	s.episodes[v.Dimension]++
	g := &OwnerGap{
		Dimension:   v.Dimension,
		Kind:        v.Kind,
		Reason:      v.Reason,
		OpenedAt:    now,
		LastSeen:    now,
		Episode:     s.episodes[v.Dimension],
		StepsByKind: map[StepKind]int{},
	}
	s.gaps[v.Dimension] = g
	out.Resolution = ResolutionOpened
	out.Gap = g.clone()
	return out
}

// ReconcileAll folds a whole classification pass into the set.
func (s *OwnerSet) ReconcileAll(vs []OwnerVerdict, now time.Time) []OwnerOutcome {
	out := make([]OwnerOutcome, 0, len(vs))
	for _, v := range vs {
		out = append(out, s.Reconcile(v, now))
	}
	return out
}

// RecordStep appends a step attempt to a standing gap. The gap stays in the
// set: steps are progress toward satisfaction, never satisfaction.
func (s *OwnerSet) RecordStep(dim OwnerDimension, rec StepRecord) (OwnerGap, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.gaps[dim]
	if g == nil {
		return OwnerGap{}, false
	}
	rec.Key = g.Key()
	g.Steps = append(g.Steps, rec)
	if g.StepsByKind == nil {
		g.StepsByKind = map[StepKind]int{}
	}
	g.StepsByKind[rec.Kind]++
	if rec.At.After(g.LastStepAt) {
		g.LastStepAt = rec.At
	}
	return g.clone(), true
}

// Open returns every standing gap, oldest first.
func (s *OwnerSet) Open() []OwnerGap {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OwnerGap, 0, len(s.gaps))
	for _, g := range s.gaps {
		out = append(out, g.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].OpenedAt.Equal(out[j].OpenedAt) {
			return out[i].OpenedAt.Before(out[j].OpenedAt)
		}
		return out[i].Dimension < out[j].Dimension
	})
	return out
}

// Get returns one standing gap.
func (s *OwnerSet) Get(dim OwnerDimension) (OwnerGap, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.gaps[dim]
	if g == nil {
		return OwnerGap{}, false
	}
	return g.clone(), true
}

// Len is the number of standing gaps.
func (s *OwnerSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.gaps)
}

// LadderGaps is the whole standing set in the 🎯T317 ladder's view.
func (s *OwnerSet) LadderGaps() []Gap {
	open := s.Open()
	out := make([]Gap, 0, len(open))
	for _, g := range open {
		out = append(out, g.LadderView())
	}
	return out
}

// OwnerDuePolicy decides whether a standing gap may take another step now.
type OwnerDuePolicy func(g OwnerGap, now time.Time) bool

// OwnerDue spaces steps by the bound that belongs to the gap's kind. A gap
// that has never been stepped is due at once: classification already waited
// out the bound before opening it, so a second full bound would only add
// latency to a plant the owner is already staring at.
func OwnerDue(b OwnerBounds) OwnerDuePolicy {
	b = b.withDefaults()
	return func(g OwnerGap, now time.Time) bool {
		if g.LastStepAt.IsZero() {
			return true
		}
		return g.SinceLastStep(now) >= b.For(g.Kind)
	}
}

// ErrOwnerStepNotApplicable is returned by an actuator whose precondition no
// longer holds (the prompt finished, the send was already acked). Drive skips
// the record entirely so a vanished condition never consumes step budget.
var ErrOwnerStepNotApplicable = errors.New("owner step not applicable")

// OwnerActuator applies one step toward closing an owner-interaction gap.
// It may report ErrOwnerStepNotApplicable to mean "nothing to do this tick".
// An actuator can never satisfy a gap.
type OwnerActuator interface {
	Step(g OwnerGap, kind StepKind, now time.Time) error
}

// OwnerActuatorFunc adapts a function to OwnerActuator.
type OwnerActuatorFunc func(g OwnerGap, kind StepKind, now time.Time) error

// Step implements OwnerActuator.
func (f OwnerActuatorFunc) Step(g OwnerGap, kind StepKind, now time.Time) error {
	return f(g, kind, now)
}

// Drive offers every standing gap its planned step, subject to the due
// policy, and records what came back. Membership is unchanged: Drive can
// only add history.
func (s *OwnerSet) Drive(now time.Time, due OwnerDuePolicy, act OwnerActuator) []StepRecord {
	if act == nil {
		return nil
	}
	if due == nil {
		due = OwnerDue(DefaultOwnerBounds())
	}
	var out []StepRecord
	for _, g := range s.Open() {
		if !due(g, now) {
			continue
		}
		kind := PlanOwnerStep(g)
		if kind == "" {
			continue
		}
		err := act.Step(g, kind, now)
		if errors.Is(err, ErrOwnerStepNotApplicable) {
			// The world moved between observation and actuation. Not a
			// failed attempt — do not spend the gap's step budget on it.
			continue
		}
		rec := StepRecord{Key: g.Key(), Kind: kind, At: now, Delivered: err == nil}
		if err != nil {
			rec.Err = err.Error()
		}
		if _, ok := s.RecordStep(g.Dimension, rec); ok {
			out = append(out, rec)
		}
	}
	return out
}
