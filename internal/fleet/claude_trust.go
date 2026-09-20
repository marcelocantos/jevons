// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"log/slog"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/claudetrust"
)

// trustConfigPath and trustOwnerRoots are the 🎯T709 seams. Nil-free
// package vars rather than struct fields because every launch road in
// this adapter goes through Launch, and a test needs to redirect the
// owner's real ~/.claude.json without a fleet fixture standing up a
// claudia registry.
var (
	trustConfigPath = claudetrust.ConfigPath
	trustOwnerRoots = claudetrust.DefaultOwnerRoots
)

// preflightTrust pre-accepts Claude Code's per-workdir trust dialog for
// a Claude seat about to be launched (🎯T709).
//
// Claude Code asks that question of a human, once per directory, before
// it will draw a composer. A tmux seat has no human: claudia presses
// Enter a few times, gives up, and the launch handshake reports a ready
// timeout — which the fleet reads as a stall and retries, and which ends
// with the seat reaped and the owner seeing only reaped_held on send.
// That is what happened to ge-po on 2026-09-20, repeatedly, on a
// freshly-cloned workdir.
//
// The answer is to have already answered. This runs before reg.Launch so
// the config the process reads at start already carries the yes.
//
// It never fails a launch. A workdir outside the owner roots is refused
// on purpose (trust is a safety boundary, and jevons does not say yes to
// arbitrary code on the owner's behalf) and an unreadable config cannot
// be repaired blind — in both cases the launch proceeds, and if the
// modal does appear, agenterr.ClassWorkspaceTrust tells the owner what
// to do instead of calling it a stall that will be retried.
func (f *Claudia) preflightTrust(name string) {
	def := f.reg.Def(name)
	if def == nil || def.Provider != claudia.ProviderClaude || def.WorkDir == "" {
		return
	}
	outcome, err := claudetrust.EnsureAccepted(trustConfigPath(), def.WorkDir, trustOwnerRoots())
	if err != nil {
		slog.Warn("🎯T709 workspace trust preflight failed",
			"agent", name, "workdir", def.WorkDir, "outcome", string(outcome), "err", err)
		return
	}
	if outcome == claudetrust.OutcomeGranted || outcome == claudetrust.OutcomeRefused {
		slog.Info("🎯T709 workspace trust preflight",
			"agent", name, "workdir", def.WorkDir, "outcome", string(outcome))
	}
}
