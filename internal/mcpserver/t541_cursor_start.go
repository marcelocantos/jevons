// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/claudia"
)

// 🎯T541 — Cursor ACP starts must not wait for prompt confirmation
// while anything that serializes MCP start is held. The incident:
// jevons_agent_start with a Cursor prompt waited for ACP confirmation
// while start was serialized, so agent_list/send/kill/event_push timed
// out. Product: Launch, release startMu, then send the brief.
//
// Claudia owns whether a Cursor conversation is resumable. This file
// must not stat provider-private session files.

// cursorRemintSeed is written when start has no opening prompt so the
// new session hosts a first user turn.
const cursorRemintSeed = "[jevons] materialize ACP session"

// Full saved-session loads with many MCP servers can take minutes. Explicit
// caller cancellation still interrupts supported startup immediately.
const defaultLaunchDeadline = 5 * time.Minute

// deferStartPrompt reports whether this provider must use start-then-send
// instead of waiting for turn confirmation on the start RPC.
func deferStartPrompt(p claudia.Provider) bool {
	return p == claudia.ProviderCursor
}

func (s *Server) launchAgent(ctx context.Context, name string) (*claudia.Agent, error) {
	agent, _, err := s.launchAgentRecovering(ctx, name)
	return agent, err
}

// launchAgentRecovering is the product launch path: lost-session and missing
// exclusive-home recovery (🎯T409 / 🎯T674) with cooperative cancellation
// (🎯T541.2 via LaunchContext). The remint, when one ran, is returned so
// agent_start can name the lost session once rather than silently resume a
// blank conversation. ReconcileProviderCaps and ExplainLaunchCapError run
// inside fleet.LaunchRecoveringReported.
func (s *Server) launchAgentRecovering(ctx context.Context, name string) (*claudia.Agent, fleet.LostSession, error) {
	if s != nil && s.launchAgentFn != nil {
		agent, err := s.launchAgentFn(ctx, name)
		return agent, fleet.LostSession{}, err
	}
	if s == nil || s.registry == nil {
		return nil, fleet.LostSession{}, fmt.Errorf("no agent registry")
	}
	return fleet.LaunchRecoveringReported(ctx, s.registry, name)
}

func (s *Server) launchWait() time.Duration {
	if s != nil && s.launchDeadline > 0 {
		return s.launchDeadline
	}
	return defaultLaunchDeadline
}

// launchAgentBounded joins cooperative startup cancellation. The registry owns
// cleanup: a returned handle may already have been running before this call.
// Legacy or non-cooperative success is preserved even after the deadline.
func (s *Server) launchAgentBounded(ctx context.Context, name string) (*claudia.Agent, error) {
	agent, _, err := s.launchAgentBoundedReported(ctx, name)
	return agent, err
}

// launchAgentBoundedReported is launchAgentBounded and also returns any remint
// (🎯T674) so the start result can name the lost session.
func (s *Server) launchAgentBoundedReported(ctx context.Context, name string) (*claudia.Agent, fleet.LostSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	wait := s.launchWait()
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	proc, lost, err := s.launchAgentRecovering(ctx, name)
	if err != nil && ctx.Err() != nil {
		return nil, lost, fmt.Errorf("launch timed out or canceled after %s (T541.2): %w", wait, ctx.Err())
	}
	return proc, lost, err
}

// startMutexHeld reports whether startMu is currently locked. Tests use
// this to prove prompt delivery runs after unlock.
func (s *Server) startMutexHeld() bool {
	if s == nil {
		return false
	}
	if !s.startMu.TryLock() {
		return true
	}
	s.startMu.Unlock()
	return false
}

// finishCursorStart runs AFTER startMu is released. It writes the opening
// brief without waiting for ACP prompt confirmation. A bound process
// after Launch is a created seat. Resume-worthiness stays in Claudia.
func (s *Server) finishCursorStart(name string, existed bool, prompt string) (briefNote string, err error) {
	_ = existed
	if s.startMutexHeld() {
		return "", fmt.Errorf("internal: finishCursorStart ran while start mutex held (🎯T541)")
	}
	text := strings.TrimSpace(prompt)
	if text == "" {
		text = cursorRemintSeed
	}
	if err := s.submitCursorStartBrief(name, text); err != nil {
		return "", fmt.Errorf("cursor ACP start did not write a conversation: %w", err)
	}
	if !s.cursorProcessBound(name) {
		return "", fmt.Errorf("cursor ACP seat has no bound process")
	}
	if strings.TrimSpace(prompt) != "" {
		return " Opening brief sent after Launch (🎯T541): start mutex released before ACP prompt delivery.", nil
	}
	return "", nil
}

func (s *Server) submitCursorStartBrief(name, prompt string) error {
	text := s.composeStartBrief(name, prompt)
	if err := s.recordAgentRequest(name, text, OriginAgent); err != nil {
		return err
	}
	var sendErr error
	if s != nil && s.cursorSubmit != nil {
		sendErr = s.cursorSubmit(name, text)
	} else if s == nil || s.registry == nil {
		return fmt.Errorf("no agent registry")
	} else {
		// The launch that just returned is a lifecycle boundary: record the
		// seat before asking whether it is alive. The seat authority's own
		// feed runs once a second, and read before it the new seat was
		// unknown, so every Cursor start failed here (journey J34).
		seatstate.ObserveRegistrySeat(s.registry, name)
		proc := s.registry.Get(name)
		if proc == nil || !(s.seatState(name).Alive == seatstate.Yes) {
			return fmt.Errorf("no bound cursor-agent for %q", name)
		}
		// ACP session/prompt is fire-and-forget: do not wait for turn
		// confirmation (that is what hung the start RPC).
		sendErr = proc.Send(text)
	}
	if sendErr == nil {
		s.observeBirthAcceptance(name, agentSendResult{Status: "sent"}, nil)
	}
	return sendErr
}

func (s *Server) composeStartBrief(name, prompt string) string {
	if s == nil {
		return strings.TrimSpace(prompt)
	}
	// roleDisplay / withIdentity take s.mu — do not hold it across them
	// (that deadlock hung TestT541FinishCursorStartReapsUnbound and
	// deafens jevons-po on a successful Cursor remint).
	roleBody := ""
	if s.registry != nil {
		if d := s.registry.Def(name); d != nil {
			roleName := s.roleDisplay(*d)
			if def, err := s.resolveRoleDef(roleName); err == nil {
				roleBody = def.Body
			}
		}
	}
	s.mu.Lock()
	if s.fleetBriefed == nil {
		s.fleetBriefed = map[string]bool{}
	}
	text, _ := EnsureFleetBriefWithRole(s.fleetBriefed, name, prompt, roleBody)
	s.mu.Unlock()
	return s.withIdentity(name, text)
}

func (s *Server) cursorProcessBound(name string) bool {
	if s != nil && s.cursorBound != nil {
		return s.cursorBound(name)
	}
	if s == nil || s.registry == nil {
		return false
	}
	proc := s.registry.Get(name)
	return proc != nil && (s.seatState(name).Alive == seatstate.Yes)
}
