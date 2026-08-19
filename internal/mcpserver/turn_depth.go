// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/turndepth"
)

// The outside half of 🎯T392.4 — the daemon watching turn depth from where
// it can see every agent, rather than from inside one.
//
// It does two things the in-band hook cannot. It RECORDS: the hook lives in
// the agent's process and writes only to that agent's own reasoning, so
// without this the ceiling would fire and leave no trace anyone could count,
// and a lever whose effect nobody can measure is indistinguishable from one
// that is off. And it is the FALLBACK for an agent the ask cannot reach — a
// Grok worker with no Claude hooks, a clone where `make turndepth` has not
// run, an agent that simply keeps calling tools. That fallback is disarmed
// by default; see turndepth.Policy.InterruptEnabled for why arming it before
// the ask is known to be landing would cost more than the depth does.
//
// The two counts are separate on purpose and are not reconciled. The hook's
// lives with the session and survives a daemon restart; this one lives with
// the daemon and survives a session rotation. Making one authoritative over
// the other would mean one of them lying whenever the other's process
// restarted, and both are only ever used to decide the same monotone
// question — has this turn gone on too long.

// progressToolUse is the ProgressType claudia stamps on every collapsed ACP
// tool_call. It is the only per-tool-call signal the daemon receives, and it
// arrives for every tool, not just the ones jevons serves.
const progressToolUse = "tool_use"

// compTurnDepth is the eventlog component for depth-ceiling decisions.
const compTurnDepth = "turn_depth"

// SetTurnDepthPolicy configures the per-turn depth ceiling. A zero Policy is
// the default ceiling with the interrupt fallback disarmed.
func (s *Server) SetTurnDepthPolicy(p turndepth.Policy) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnDepthPolicy = p
	if s.turnDepth == nil {
		s.turnDepth = turndepth.NewCounter()
	}
}

// SetTurnDepthInterrupter overrides how the fallback cuts a turn. Test seam.
func (s *Server) SetTurnDepthInterrupter(fn func(string) error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnDepthInterrupt = fn
}

// interruptTurn cuts name's running turn, reporting whether there was
// anything to cut. The default resolves a LIVE process and nothing else:
// rehydrating an agent in order to interrupt it would start the very turn
// the ceiling is trying to end.
func (s *Server) interruptTurn(name string) (bool, error) {
	s.mu.Lock()
	fn := s.turnDepthInterrupt
	s.mu.Unlock()
	if fn != nil {
		return true, fn(name)
	}
	proc := s.lookupAgentProc(name)
	if proc == nil {
		return false, nil
	}
	return true, proc.Interrupt()
}

// turnDepthState returns the counter and policy together, creating the
// counter on first use so a Server built without SetTurnDepthPolicy still
// counts (and still records) with the defaults.
func (s *Server) turnDepthState() (*turndepth.Counter, turndepth.Policy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnDepth == nil {
		s.turnDepth = turndepth.NewCounter()
	}
	return s.turnDepth, s.turnDepthPolicy
}

// observeTurnDepth applies one agent event to the depth ceiling.
//
// Called from agentEventSink for every event, and deliberately cheap for the
// ones it ignores: an agent's event stream is the wire every control in this
// daemon hangs off (🎯T426), and work done on it is work not being done on
// somebody's turn ending.
func (s *Server) observeTurnDepth(name string, ev claudia.Event) {
	if s == nil {
		return
	}
	counter, pol := s.turnDepthState()

	if ev.IsTerminalStop() {
		st := counter.EndTurn(name)
		// 🎯T471: latch before any early return so EndTurn's Requested
		// flag still protects the seat from auto-reap after the counter
		// forgets the turn.
		if st.Requested {
			s.noteCheckpointEnded(name)
		}
		if st.Calls == 0 {
			return
		}
		// Every turn's final depth, over the ceiling or not, because the
		// acceptance this target is measured against is a HISTOGRAM: "the
		// affected tail shrinks without the 1-7 call band changing" is not a
		// claim the over-ceiling turns alone can settle.
		s.logLifecycle(compTurnDepth, "turn_ended", "ok", map[string]any{
			"agent": name, "calls": st.Calls, "ceiling": pol.EffectiveCeiling(),
			"asked": st.Requested, "interrupted": st.Interrupted,
		})
		if st.Requested {
			slog.Info("turn depth ceiling — turn ended after checkpoint ask",
				"agent", name, "calls", st.Calls, "ceiling", pol.EffectiveCeiling(),
				"interrupted", st.Interrupted)
		}
		if turndepth.ShouldResumeAfterCheckpoint(st) {
			s.scheduleCheckpointResume(name, st)
		}
		return
	}

	if ev.Type != "progress" || ev.ProgressType != progressToolUse {
		return
	}

	d := pol.Evaluate(counter.Tool(name))
	switch d.Action {
	case turndepth.ActionRequestCheckpoint:
		counter.MarkRequested(name)
		// The daemon does not deliver this ask — the hook inside the agent's
		// own process does, and it has already done so by the time this event
		// arrives. What is recorded here is that the ceiling was reached,
		// which is true whether or not any hook was installed to say it.
		slog.Info("turn depth ceiling reached — checkpoint asked",
			"agent", name, "calls", d.Calls, "ceiling", d.Ceiling, "reason", d.Reason)
		s.logLifecycle(compTurnDepth, "checkpoint_asked", "ok", map[string]any{
			"agent": name, "calls": d.Calls, "ceiling": d.Ceiling, "reason": d.Reason,
		})
	case turndepth.ActionInterrupt:
		// Latch before cutting, not after: an interrupt that returns an error
		// may still have landed, and a turn re-cut on every subsequent call
		// would be a loop rather than an enforcement.
		counter.MarkInterrupted(name)
		// WARN, not Info: this is the path the design tries not to take, and
		// a fallback that fires routinely is a signal the ask is not landing
		// — which is a wiring answer, not something more interrupts fix.
		slog.Warn("turn depth fallback — interrupting a turn that ignored the checkpoint ask",
			"agent", name, "calls", d.Calls, "ceiling", d.Ceiling, "reason", d.Reason)
		live, err := s.interruptTurn(name)
		switch {
		case !live:
			slog.Warn("turn depth fallback: no live process to interrupt",
				"agent", name, "calls", d.Calls, "ceiling", d.Ceiling)
			s.logLifecycle(compTurnDepth, "interrupt", "error", map[string]any{
				"agent": name, "calls": d.Calls, "ceiling": d.Ceiling,
				"err": "no live process",
			})
		case err != nil:
			slog.Error("turn depth interrupt failed",
				"agent", name, "calls", d.Calls, "err", err)
			s.logLifecycle(compTurnDepth, "interrupt", "error", map[string]any{
				"agent": name, "calls": d.Calls, "ceiling": d.Ceiling, "err": err.Error(),
			})
		default:
			s.logLifecycle(compTurnDepth, "interrupt", "ok", map[string]any{
				"agent": name, "calls": d.Calls, "ceiling": d.Ceiling, "reason": d.Reason,
			})
		}
	}
}

// forgetTurnDepth drops an agent's turn record when it leaves the fleet, so
// a long-lived daemon does not accumulate turns for names that no longer
// exist.
// SetTurnDepthResumer overrides how a checkpointed turn is continued.
// Test seam; the product path sends ResumePrompt to the same agent.
func (s *Server) SetTurnDepthResumer(fn func(name, prompt string)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnDepthResume = fn
}

func (s *Server) scheduleCheckpointResume(name string, st turndepth.State) {
	if s == nil || name == "" {
		return
	}
	prompt := turndepth.ResumePrompt(st)
	s.mu.Lock()
	fn := s.turnDepthResume
	s.mu.Unlock()
	s.logLifecycle(compTurnDepth, "checkpoint_resume", "ok", map[string]any{
		"agent": name, "calls": st.Calls,
	})
	if fn != nil {
		fn(name, prompt)
		return
	}
	go s.sendCheckpointResume(name, prompt)
}

func (s *Server) sendCheckpointResume(name, prompt string) {
	if s == nil || strings.TrimSpace(prompt) == "" {
		return
	}
	s.sendDaemonComposed(name, prompt, false)
}

func (s *Server) forgetTurnDepth(name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	c := s.turnDepth
	delete(s.checkpointEnded, name)
	s.mu.Unlock()
	c.Forget(name)
}

// noteCheckpointEnded records that name's just-ended turn hit the 🎯T392.4
// depth-ceiling ask. Consumed by maybeReapDoneWorkAgent (🎯T471).
func (s *Server) noteCheckpointEnded(name string) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkpointEnded == nil {
		s.checkpointEnded = map[string]bool{}
	}
	s.checkpointEnded[name] = true
}

// consumeCheckpointEnded reports and clears the 🎯T471 latch for name.
func (s *Server) consumeCheckpointEnded(name string) bool {
	if s == nil || name == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.checkpointEnded[name] {
		return false
	}
	delete(s.checkpointEnded, name)
	return true
}
