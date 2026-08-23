// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/converge"
	"github.com/marcelocantos/jevons/internal/converge/attenuate"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// ImpatienceEngine is the daemon-side 🎯T317 ladder: a 🎯T316 reconcile set,
// a Ladder with 🎯T318 attenuation, the three impure actuators, and the
// 🎯T319 postmortem journal/sink. The pure packages decide policy; this type
// is the only place that talks to the fleet.
//
// Driven from idlePressureSweep so every open-mission idle tick both updates
// the standing set and actuates the due rung. Safe for concurrent use: the
// converge loop is single-threaded in production, but Set is already concurrent
// and the ladder is guarded here.
type ImpatienceEngine struct {
	mu         sync.Mutex
	set        *converge.Set
	ladder     *converge.Ladder
	sinks      converge.Sinks
	postmortem converge.PostmortemSink
	journal    *converge.PostmortemJournal
	overseer   string
}

// ImpatienceEngineArgs constructs the engine. Sinks may be partially nil —
// an unwired rung errors at fire time rather than failing silently.
// Postmortem may be nil: closed incidents stay pending until a sink is wired
// (PostmortemJournal.Flush treats nil as leave-pending).
type ImpatienceEngineArgs struct {
	Sinks      converge.Sinks
	Postmortem converge.PostmortemSink // 🎯T319 owner-visible close report as root
	Overseer   string                  // empty → "jevons"
}

// NewImpatienceEngine builds a standing set + ladder with default attenuation.
func NewImpatienceEngine(args ImpatienceEngineArgs) *ImpatienceEngine {
	l := converge.NewLadder()
	l.SetAttenuator(attenuate.NewAttenuator(attenuate.DefaultPolicy()))
	overseer := strings.TrimSpace(args.Overseer)
	if overseer == "" {
		overseer = "jevons"
	}
	return &ImpatienceEngine{
		set:        converge.NewSet(),
		ladder:     l,
		sinks:      args.Sinks,
		postmortem: args.Postmortem,
		journal:    converge.NewPostmortemJournal(),
		overseer:   overseer,
	}
}

// SetImpatienceEngine installs the 🎯T317 ladder into the idle-pressure sweep.
// Nil clears it and restores the T315-only SweepIdleNudges path.
func (s *Server) SetImpatienceEngine(e *ImpatienceEngine) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.impatience = e
	s.mu.Unlock()
}

// impatienceEngine returns the installed engine, or nil.
func (s *Server) impatienceEngine() *ImpatienceEngine {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.impatience
}

// rePressureSink delivers the 🎯T315 actuator path for one agent: the same
// brief-or-continue wire as SweepIdleNudges, fire-and-forget.
type rePressureSink struct {
	server *Server
}

// RePressure implements converge.RePressureSink.
func (r *rePressureSink) RePressure(agent, mission string) error {
	if r == nil || r.server == nil {
		return fmt.Errorf("repressure: no server")
	}
	s := r.server
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return fmt.Errorf("repressure: empty agent")
	}

	// 🎯T412: a seat whose registry row claims a conversation that is not on
	// disk is dead, and a nudge typed into it lands in a void. Refusing here
	// makes the tick report the seat as a failure (actions counted with
	// errors) instead of actions=N errors=0 forever — the 2026-08-10 shape,
	// where seven nudges a tick went to six ghosts and the ladder read green.
	// Fresh and rotated seats (Materialized=false) are not refused: a full
	// re-brief is exactly their cure, and the T416 confirm machinery judges
	// whether it landed.
	if s.registry != nil && !s.agentHasTurnBegan(agent) {
		if def := s.registry.Def(agent); def != nil && fleet.SessionLost(def) {
			return fmt.Errorf("repressure refused: agent %q is %s — its session %s has no conversation on disk (🎯T412); recover or reap it rather than nudging",
				agent, AgentStatusDeadUnmaterialized, def.SessionID)
		}
	}

	briefPresent := false
	s.mu.Lock()
	if s.fleetBriefed != nil && s.fleetBriefed[agent] {
		briefPresent = true
	}
	ledger := s.idleNudgeLedger
	activity := s.idleActivity
	s.mu.Unlock()

	kind := ClassifyIdleNudgeKind(briefPresent)
	text := FormatIdleNudgeText(IdleNudgeTextArgs{
		Name:     agent,
		TargetID: mission,
		Reason:   "impatience_ladder_repressure",
		Kind:     kind,
	})
	event := IdleNudgeEventSource(false, kind)
	wire := formatIdleNudgeWire(event, text)
	if _, err := s.sendToAgent(agent, wire, false); err != nil {
		return err
	}
	if kind == IdleNudgeKindFullBrief {
		s.mu.Lock()
		if s.fleetBriefed == nil {
			s.fleetBriefed = map[string]bool{}
		}
		s.fleetBriefed[agent] = true
		s.mu.Unlock()
	}
	if ledger != nil {
		if err := ledger.Record(agent, time.Now()); err != nil {
			slog.Warn("impatience re-pressure ledger record failed", "agent", agent, "err", err)
		}
	}
	if activity != nil {
		activity.ClearRecover(agent, time.Now())
	}
	return nil
}

// overseerSink pushes converge-impatient/-failing events to the root overseer.
// Fire-and-forget send — never butler.PushEvent (blocks up to ~10m).
type overseerSink struct {
	server   *Server
	overseer string
}

// PushConvergeEvent implements converge.OverseerSink.
func (o *overseerSink) PushConvergeEvent(class, text string) error {
	if o == nil || o.server == nil {
		return fmt.Errorf("overseer sink: no server")
	}
	target := strings.TrimSpace(o.overseer)
	if target == "" {
		target = "jevons"
	}
	class = strings.TrimSpace(class)
	if class == "" {
		class = converge.EventClassImpatient
	}
	wire := butler.FormatEventPush(class, text)
	_, err := o.server.sendToAgent(target, wire, false)
	life := map[string]any{
		"target": target,
		"class":  class,
		"event":  class,
	}
	if err != nil {
		life["err"] = err.Error()
		o.server.logLifecycle(compEventPush, "converge", "error", life)
		return err
	}
	o.server.logLifecycle(compEventPush, "converge", "ok", life)
	slog.Info("impatience overseer noise", "class", class, "target", target)
	return nil
}

// NewImpatienceSinks builds the three impure edges for the ladder.
// human may be nil (unwired top rung → Actuate errors per fire).
func NewImpatienceSinks(s *Server, overseer string, human converge.HumanSink) converge.Sinks {
	if overseer == "" {
		overseer = "jevons"
	}
	return converge.Sinks{
		RePressure: &rePressureSink{server: s},
		Overseer:   &overseerSink{server: s, overseer: overseer},
		Human:      human,
	}
}

// tick runs one observe → reconcile set → ladder → actuate pass.
// deps mirrors idlePressureSweep hermetic overrides.
func (e *ImpatienceEngine) tick(s *Server, deps idlePressureDeps, hooks IdlePressureHooks) {
	if e == nil || s == nil || s.registry == nil {
		return
	}
	now := deps.Now
	if now.IsZero() {
		now = time.Now()
	}
	overseer := e.overseer
	if overseer == "" {
		overseer = "jevons"
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	defs := s.registry.List()
	present := make(map[string]bool, len(defs))
	refusalBy := make(map[string]bool, len(defs))
	var outcomes []converge.Outcome
	for _, d := range defs {
		if d.Name == "" || d.Name == overseer {
			continue
		}
		present[d.Name] = true
		obs := s.observeForImpatience(d, hooks, deps, now, defs)
		refusalBy[d.Name] = obs.RefusalHold
		out := e.set.Reconcile(obs, now)
		outcomes = append(outcomes, out)
		// 🎯T454: phase=working under a refusal hold is not progress — it is
		// the agent thrashing against a wall.
		if strings.EqualFold(strings.TrimSpace(obs.Phase), "working") && !obs.RefusalHold {
			e.ladder.ObserveProgress(d.Name, attenuate.SignalTargetWorking, now)
		}
	}

	// Agents that left the registry while still tracked: reap-shaped observation
	// so the set can close them (satisfaction via agent_reaped).
	for _, g := range e.set.Open() {
		if present[g.Agent] {
			continue
		}
		out := e.set.Reconcile(converge.Observation{
			Name:        g.Agent,
			Purpose:     "work",
			Reaped:      true,
			MissionOpen: true,
			TargetID:    g.Mission,
		}, now)
		outcomes = append(outcomes, out)
	}

	// Ladder view: standing gaps (unsatisfied by construction) plus any
	// satisfaction verdicts from this tick so incidents close as Satisfied
	// rather than Departed when the agent is still present and working.
	gaps := e.set.LadderGaps()
	for _, o := range outcomes {
		if g, ok := o.LadderView(); ok {
			gaps = append(gaps, g)
		}
	}
	// 🎯T414: stamp every gap with the deliberate intent behind its agent.
	// The ladder holds a gap whose intent declines rather than firing at it —
	// and holds it *without* closing the incident, because a park resolves
	// nothing. On 2026-08-10 this ladder repressured three parked workers
	// twice each and then reported the incidents cleared.
	intent := s.fleetIntent()
	for i := range gaps {
		gaps[i].FleetIntent = intent.FleetState()
		gaps[i].Intent = intent.AgentState(gaps[i].Agent)
		// 🎯T454: suppress rungs while turns are refusal-only, or while the
		// fleet is under a provider wall (T406) — either signal is enough.
		if refusalBy[gaps[i].Agent] || gaps[i].FleetIntent == fleetintent.BlockedProvider {
			gaps[i].RefusalOnly = true
		}
	}

	actions, closed := e.ladder.Reconcile(now, gaps)
	pendingPostmortem := e.journal != nil && len(e.journal.Pending()) > 0
	if len(actions) == 0 && len(closed) == 0 && !pendingPostmortem {
		return
	}

	// Actuate one-by-one so step records know delivered vs failed. Actuate
	// returns only failures (not a parallel slice), so batch would lose the
	// per-action verdict.
	var nErr int
	for _, a := range actions {
		errs := converge.Actuate([]converge.Action{a}, e.sinks)
		delivered := len(errs) == 0
		errStr := ""
		if !delivered {
			nErr++
			errStr = errs[0].Error()
			slog.Warn("impatience actuate", "err", errs[0], "agent", a.Agent, "rung", a.Rung.String())
			s.logLifecycle(compIdleNudge, "impatience", "error", map[string]any{
				"agent": a.Agent, "rung": a.Rung.String(), "err": errStr,
			})
		}
		if a.Kind == converge.ActFire {
			if kind := stepKindForAction(a); kind != "" {
				e.set.RecordStep(converge.KeyFor(a.Agent), converge.StepRecord{
					Kind: kind, At: now, Delivered: delivered, Err: errStr,
				})
			}
			if delivered && a.Rung == converge.RungRepressure {
				e.ladder.ObserveProgress(a.Agent, attenuate.SignalRepressureDelivered, now)
			}
		}
	}
	if len(actions) > 0 {
		slog.Info("impatience ladder tick",
			"actions", len(actions), "errors", nErr, "closed", len(closed), "open_gaps", e.set.Len())
		s.logLifecycle(compIdleNudge, "impatience", "ok", map[string]any{
			"actions": len(actions), "errors": nErr, "closed": len(closed), "open": e.set.Len(),
		})
	}

	// 🎯T319: every closed episode → exactly one owner-visible mini-postmortem
	// as root, independent of pathway. Clear human chrome is already in
	// actions via ActClearHuman; the report is never satisfaction.
	if e.journal != nil {
		if _, err := e.journal.Flush(closed, e.postmortem); err != nil {
			slog.Warn("impatience postmortem deliver", "err", err, "closed", len(closed))
			s.logLifecycle(compIdleNudge, "impatience_postmortem", "error", map[string]any{
				"closed": len(closed), "err": err.Error(),
			})
		}
	}
}

func stepKindForAction(a converge.Action) converge.StepKind {
	if a.Kind == converge.ActClearHuman {
		return "" // withdrawal is not a step toward closing; gap already closed
	}
	switch a.Rung {
	case converge.RungRepressure:
		return converge.StepRePressure
	case converge.RungOverseerNoise:
		return converge.StepOverseerNoise
	case converge.RungHumanAlert:
		return converge.StepHumanAlert
	}
	return ""
}

// observeForImpatience projects one registry row into a T316 Observation.
func (s *Server) observeForImpatience(
	d claudia.AgentDef,
	hooks IdlePressureHooks,
	deps idlePressureDeps,
	now time.Time,
	defs []claudia.AgentDef,
) converge.Observation {
	purpose := strings.TrimSpace(d.Purpose)
	if purpose == "" {
		purpose = claudia.PurposeWork
	}

	running := false
	if deps.Running != nil {
		running = deps.Running(d.Name)
	} else if proc := s.registry.Get(d.Name); proc != nil {
		running = proc.Alive()
	}
	deliberateStop := !running && !d.AutoStart

	phase := ""
	refusalHold := false
	substantivePulse := false
	s.mu.Lock()
	if s.idleActivity != nil {
		act := s.idleActivity.Get(d.Name)
		phase = act.Phase
		refusalHold = act.RefusalHold
	}
	s.mu.Unlock()
	if s.idleActivity != nil {
		substantivePulse = s.idleActivity.ConsumeTurnPulses(d.Name)
	}

	targetID := strings.TrimSpace(d.TargetID)
	missionOpen := false
	missionClosed := false
	if targetID != "" {
		if hooks.MissionOpen != nil {
			missionOpen = hooks.MissionOpen(targetID)
			// Bound target no longer open ⇒ ledger closed/reaped evidence.
			missionClosed = !missionOpen
		} else {
			missionOpen = true
		}
	} else if purpose == claudia.PurposeWork {
		// Unbound implementer residual (🎯T244 / T316): open until accounted for.
		// 🎯T330: PO/boss with engaged children is not open for parent thrash.
		phaseOf := func(name string) string {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.idleActivity == nil {
				return ""
			}
			return s.idleActivity.Get(name).Phase
		}
		missionOpen = HasOpenMissionForIdle(d, hooks.MissionOpen,
			CountWorkChildren(defs, d.Name),
			CountEngagedWorkChildren(defs, d.Name, phaseOf, hooks.MissionOpen))
	}

	designGated := false
	if hooks.DesignGated != nil && targetID != "" {
		designGated = hooks.DesignGated(targetID)
	}

	claimsDone := false
	if hooks.LooksSatisfied != nil {
		if r := hooks.LooksSatisfied(d.Name); r != "" {
			claimsDone = LooksLikeFinishedWorkReport(r)
		}
	}

	_ = now // reserved for future idle-age observation fields
	return converge.Observation{
		Name:            d.Name,
		Purpose:         purpose,
		Phase:           phase,
		ProcessRunning:  running,
		TargetID:        targetID,
		MissionOpen:     missionOpen,
		MissionClosed:   missionClosed,
		DeliberateStop:  deliberateStop,
		DesignGated:     designGated,
		ClaimsDone:      claimsDone,
		RefusalHold:     refusalHold && !substantivePulse,
		SubstantiveTurn: substantivePulse,
	}
}
