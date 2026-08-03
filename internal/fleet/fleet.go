// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fleet is the claudia-backed implementation of butler.Fleet:
// launches, directs, and stops disposable agent processes behind
// durable threads. It also implements butler.Participants for agents
// that exist only in the registry (🎯T114 unified deliver path).
package fleet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/thread"
)

// Default timeouts for the launch handshake and a directed turn's reply.
const (
	defaultReadyTimeout = 45 * time.Second
	defaultReplyTimeout = 10 * time.Minute
)

// Claudia adapts a claudia.Registry to the butler.Fleet interface and
// to butler.Participants (agent-only deliver).
type Claudia struct {
	reg             *claudia.Registry
	defaultProvider claudia.Provider
	readyTimeout    time.Duration
	replyTimeout    time.Duration
}

// NewClaudia wraps a registry as a Fleet. Default provider resolves from
// env / Grok (🎯T148); main should call SetDefaultProvider with the
// config-resolved value.
func NewClaudia(reg *claudia.Registry) *Claudia {
	return &Claudia{
		reg:             reg,
		defaultProvider: cli.ResolveProvider("", ""),
		readyTimeout:    defaultReadyTimeout,
		replyTimeout:    defaultReplyTimeout,
	}
}

// SetDefaultProvider sets the daemon-wide backend for new threads when
// the thread record has no provider (🎯T148).
func (f *Claudia) SetDefaultProvider(p claudia.Provider) {
	if p != "" {
		f.defaultProvider = p
	}
}

// providerForLaunch picks the registry provider for a thread Launch.
// Never clobbers a non-empty stored provider (resume keeps backend).
func providerForLaunch(stored, fromThread, defaultProv claudia.Provider) claudia.Provider {
	return cli.SelectAgentProvider(string(fromThread), stored, defaultProv)
}

// ensureRegistered mints or backfills the registry row for a thread without
// spawning a process. Dual-write half of Launch (🎯T114/T148) and hermetic
// surface for 🎯T215 provider=claude Session stitch tests.
//
// Provider is set on mint or backfilled when empty; never forced to Grok on
// resume when a stored provider exists. Materialized stays false until a real
// (or fake-backend) Launch succeeds inside claudia.Registry.
func (f *Claudia) ensureRegistered(t *thread.Thread) error {
	purpose := strings.TrimSpace(t.Purpose)
	if purpose == "" {
		purpose = claudia.PurposeAside // thread path → aside by default
	}
	threadProv := claudia.Provider(strings.TrimSpace(t.Provider))

	// Ensure a registry def. Resume when SessionID is known; otherwise
	// mint a placeholder id and let the provider replace it on session/new.
	if f.reg.Def(t.ID) == nil {
		sid := t.SessionID
		if sid == "" {
			sid = uuid.New().String()
		}
		prov := providerForLaunch("", threadProv, f.defaultProvider)
		if err := f.reg.Register(claudia.AgentDef{
			Name:      t.ID,
			WorkDir:   t.WorkDir,
			Model:     t.Model,
			Provider:  prov,
			SessionID: sid,
			AutoStart: true,
			Parent:    t.Parent,
			Purpose:   purpose,
		}); err != nil {
			return fmt.Errorf("register agent %q: %w", t.ID, err)
		}
		if t.SessionID == "" {
			t.SessionID = sid
		}
		return nil
	}

	def := f.reg.Def(t.ID)
	if def == nil {
		return nil
	}
	dirty := false
	// Backfill empty provider only — never overwrite a stored choice.
	if def.Provider == "" {
		def.Provider = providerForLaunch("", threadProv, f.defaultProvider)
		dirty = true
	}
	// Backfill empty parent when the spawn path now knows the creator.
	if def.Parent == "" && t.Parent != "" {
		def.Parent = t.Parent
		dirty = true
	}
	// Backfill purpose for legacy dual-write rows (🎯T114).
	if def.Purpose == "" && purpose != "" {
		def.Purpose = purpose
		dirty = true
	}
	if dirty {
		if err := f.reg.Register(*def); err != nil {
			return fmt.Errorf("update agent %q: %w", t.ID, err)
		}
	}
	return nil
}

// Launch ensures a live, ready process for the thread. If the thread's
// session already exists on disk, claudia resumes it (--resume); the
// resume/summary menu is auto-cleared by claudia's readiness handshake
// (T24). It populates t.SessionID with the live process's session so
// the thread can be rehydrated later.
//
// Dual-write (🎯T114): every thread Launch registers or updates the
// agent registry row with Parent + Purpose so threads and agents share
// one id space. Parent lineage (🎯T111.3) is taken from the thread.
// Provider (🎯T148) is set on mint or backfilled when empty; never forced
// to Grok on resume when a stored provider exists.
func (f *Claudia) Launch(t *thread.Thread) error {
	if err := f.ensureRegistered(t); err != nil {
		return err
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

	if sid := ag.SessionID(); sid != "" {
		t.SessionID = sid
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

// Remove stops the process and drops the registry definition entirely, so
// it won't auto-restart. The underlying Grok session on disk is left
// intact (only jevons's ownership is dropped).
func (f *Claudia) Remove(id string) {
	f.reg.Stop(id)
	if err := f.reg.Remove(id); err != nil {
		// A thread with no registry def (observe-only) is a normal no-op.
		return
	}
}

// Exists reports whether a fleet agent is registered (butler.Participants).
func (f *Claudia) Exists(id string) bool {
	if f == nil || f.reg == nil || id == "" {
		return false
	}
	return f.reg.Def(id) != nil
}

// Deliver rehydrates a registered agent if needed and sends text,
// waiting for a reply (butler.Participants — 🎯T114 / 🎯T111.2).
func (f *Claudia) Deliver(id, text string) (string, error) {
	if f == nil || f.reg == nil {
		return "", fmt.Errorf("no agent registry")
	}
	if f.reg.Def(id) == nil {
		return "", fmt.Errorf("no agent %q", id)
	}
	ag := f.reg.Get(id)
	if ag == nil || !ag.Alive() {
		launched, err := f.reg.Launch(id)
		if err != nil {
			return "", fmt.Errorf("could not rehydrate agent %q: %w", id, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), f.readyTimeout)
		defer cancel()
		if err := launched.WaitReady(ctx); err != nil {
			return "", fmt.Errorf("agent %q not ready: %w", id, err)
		}
		ag = launched
	}
	if err := ag.Send(text); err != nil {
		return "", fmt.Errorf("send to agent %q: %w", id, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), f.replyTimeout)
	defer cancel()
	reply, err := ag.WaitForResponse(ctx)
	if err != nil {
		return "", fmt.Errorf("await reply from agent %q: %w", id, err)
	}
	return reply, nil
}
