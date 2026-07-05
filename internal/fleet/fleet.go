// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fleet is the claudia-backed implementation of butler.Fleet:
// the mechanism that launches, directs, and stops the disposable
// Claude Code processes behind durable threads. It keeps the claudia
// dependency out of the butler's policy layer.
package fleet

import (
	"context"
	"fmt"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/thread"
)

// Default timeouts for the launch handshake and a directed turn's reply.
const (
	defaultReadyTimeout = 45 * time.Second
	defaultReplyTimeout = 10 * time.Minute
)

// Claudia adapts a claudia.Registry to the butler.Fleet interface.
type Claudia struct {
	reg          *claudia.Registry
	readyTimeout time.Duration
	replyTimeout time.Duration
}

// NewClaudia wraps a registry as a Fleet.
func NewClaudia(reg *claudia.Registry) *Claudia {
	return &Claudia{
		reg:          reg,
		readyTimeout: defaultReadyTimeout,
		replyTimeout: defaultReplyTimeout,
	}
}

// Launch ensures a live, ready process for the thread. If the thread's
// session already exists on disk, claudia resumes it (--resume); the
// resume/summary menu is auto-cleared by claudia's readiness handshake
// (T24). It populates t.SessionID with the live process's session so
// the thread can be rehydrated later.
func (f *Claudia) Launch(t *thread.Thread) error {
	// Ensure a registry def. When the thread already carries a session id
	// — a taken-over adopted session, or a rehydrate whose registry def
	// was lost — register the def WITH that id so claudia resumes that
	// exact conversation (--resume) rather than minting a fresh one. A
	// brand-new spawn (no session id) lets claudia mint one.
	if f.reg.Def(t.ID) == nil {
		if t.SessionID != "" {
			if err := f.reg.Register(claudia.AgentDef{
				Name:      t.ID,
				WorkDir:   t.WorkDir,
				Model:     t.Model,
				SessionID: t.SessionID,
				AutoStart: true,
			}); err != nil {
				return fmt.Errorf("register agent %q: %w", t.ID, err)
			}
		} else if _, err := f.reg.EnsureAgent(t.ID, t.WorkDir, t.Model, true); err != nil {
			return fmt.Errorf("register agent %q: %w", t.ID, err)
		}
	}
	ag, err := f.reg.Launch(t.ID)
	if err != nil {
		return fmt.Errorf("launch agent %q: %w", t.ID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.readyTimeout)
	defer cancel()
	if err := ag.WaitReady(ctx); err != nil {
		return fmt.Errorf("agent %q not ready: %w", t.ID, err)
	}

	if t.SessionID == "" {
		t.SessionID = ag.SessionID()
	}
	return nil
}

// Send delivers a turn to the thread's live process and waits for its
// reply. It requires a live process (call Launch first).
func (f *Claudia) Send(id, text string) (string, error) {
	ag := f.reg.Get(id)
	if ag == nil || !ag.Alive() {
		return "", fmt.Errorf("no live process for thread %q", id)
	}
	if err := ag.Send(text); err != nil {
		return "", fmt.Errorf("send to %q: %w", id, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.replyTimeout)
	defer cancel()
	reply, err := ag.WaitForResponse(ctx)
	if err != nil {
		return "", fmt.Errorf("await reply from %q: %w", id, err)
	}
	return reply, nil
}

// Alive reports whether a live process currently exists for the thread.
func (f *Claudia) Alive(id string) bool {
	ag := f.reg.Get(id)
	return ag != nil && ag.Alive()
}

// Stop stops the thread's process resumably; the registry retains its
// definition (and session id) so a later Launch rehydrates it.
func (f *Claudia) Stop(id string) {
	f.reg.Stop(id)
}
