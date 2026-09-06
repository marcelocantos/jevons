// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T541 — Cursor ACP remints must materialize a live conversation
// (store.db + bound cursor-agent) without holding the start mutex across
// prompt delivery. The incident: jevons_agent_start with a Cursor prompt
// waited for ACP confirmation while start was serialized, so
// agent_list/send/kill/event_push timed out. The workaround that is now
// product: Launch, release startMu, then send the brief.

// cursorRemintSeed is written when start has no opening prompt so
// session/new (first mint or load-fail remint) still hosts a conversation
// the next Launch can resume.
const cursorRemintSeed = "[jevons] materialize ACP session"

const defaultCursorMaterializeWait = 2 * time.Second

// Full saved-session loads with many MCP servers can take minutes. Explicit
// caller cancellation still interrupts supported startup immediately.
const defaultLaunchDeadline = 5 * time.Minute

// deferStartPrompt reports whether this provider must use start-then-send
// instead of waiting for turn confirmation on the start RPC.
func deferStartPrompt(p claudia.Provider) bool {
	return p == claudia.ProviderCursor
}

// CursorSeatMaterialized is the hermetic oracle: a seat is materialized
// only when store.db (or equivalent) exists AND a process is bound.
// Meta-only registry rows fail this.
func CursorSeatMaterialized(storeExists, processBound bool) bool {
	return storeExists && processBound
}

func (s *Server) launchAgent(ctx context.Context, name string) (*claudia.Agent, error) {
	if s != nil && s.launchAgentFn != nil {
		return s.launchAgentFn(ctx, name)
	}
	if s == nil || s.registry == nil {
		return nil, fmt.Errorf("no agent registry")
	}
	if contextual, ok := any(s.registry).(interface {
		LaunchContext(context.Context, string) (*claudia.Agent, error)
	}); ok {
		return contextual.LaunchContext(ctx, name)
	}
	// The published Claudia pin predates cancellation. Keep it buildable,
	// but do not pretend its synchronous legacy operation has a hard deadline.
	slog.Warn("Claudia dependency lacks cancellable agent startup", "name", name)
	return s.registry.Launch(name)
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
	if ctx == nil {
		ctx = context.Background()
	}
	wait := s.launchWait()
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	proc, err := s.launchAgent(ctx, name)
	if err != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("launch timed out or canceled after %s (T541.2): %w", wait, ctx.Err())
	}
	return proc, err
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
// conversation (brief or remint seed) without waiting for ACP prompt
// confirmation, then requires store.db + a bound process or fails loud.
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
		return "", fmt.Errorf("cursor ACP remint did not write a conversation: %w", err)
	}
	store, bound := s.waitCursorMaterialized(name)
	if !CursorSeatMaterialized(store, bound) {
		return "", fmt.Errorf("cursor ACP seat unmaterialized (store.db=%v process_bound=%v) — refusing idle-zombie", store, bound)
	}
	if s.registry != nil {
		if merr := s.registry.MarkMaterialized(name); merr != nil {
			slog.Warn("cursor MarkMaterialized after store+bound", "name", name, "err", merr)
		}
	}
	if strings.TrimSpace(prompt) != "" {
		return " Opening brief sent after Launch (🎯T541): start mutex released before ACP prompt delivery.", nil
	}
	return "", nil
}

func slogCursorMaterialize(name string, err error) {
	// MarkMaterialized can fail for a missing row in hermetics; the
	// store+bound check already decided the seat is real.
	_ = name
	_ = err
}

func (s *Server) submitCursorStartBrief(name, prompt string) error {
	text := s.composeStartBrief(name, prompt)
	if err := s.recordAgentRequest(name, text, OriginAgent); err != nil {
		return err
	}
	if s != nil && s.cursorSubmit != nil {
		return s.cursorSubmit(name, text)
	}
	if s == nil || s.registry == nil {
		return fmt.Errorf("no agent registry")
	}
	proc := s.registry.Get(name)
	if proc == nil || !proc.Alive() {
		return fmt.Errorf("no bound cursor-agent for %q", name)
	}
	// ACP session/prompt is fire-and-forget: do not wait for turn
	// confirmation (that is what hung the start RPC).
	return proc.Send(text)
}

func (s *Server) composeStartBrief(name, prompt string) string {
	if s == nil {
		return strings.TrimSpace(prompt)
	}
	// roleDisplay / withIdentity take s.mu — do not hold it across them
	// (that deadlock hung TestT541FinishCursorStartReapsMetaOnly and
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

func (s *Server) waitCursorMaterialized(name string) (store, bound bool) {
	wait := s.cursorWait()
	deadline := time.Now().Add(wait)
	for {
		store, bound = s.observeCursorSeat(name)
		if CursorSeatMaterialized(store, bound) || !time.Now().Before(deadline) {
			return store, bound
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (s *Server) cursorWait() time.Duration {
	if s != nil && s.cursorObserve != nil {
		return 0
	}
	if s != nil && s.cursorMaterializeWait > 0 {
		return s.cursorMaterializeWait
	}
	return defaultCursorMaterializeWait
}

func (s *Server) observeCursorSeat(name string) (store, bound bool) {
	if s != nil && s.cursorObserve != nil {
		return s.cursorObserve(name)
	}
	if s == nil || s.registry == nil {
		return false, false
	}
	proc := s.registry.Get(name)
	bound = proc != nil && proc.Alive()
	if d := s.registry.Def(name); d != nil && d.SessionID != "" {
		p := claudia.CursorACPStorePath(d.SessionID)
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			store = true
		}
	}
	return store, bound
}
