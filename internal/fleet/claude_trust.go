// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"log/slog"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/claudetrust"
	"github.com/marcelocantos/jevons/internal/seatprovider"
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

// PreflightTrustRegistry pre-accepts Claude Code's per-workdir trust
// dialog for a Claude seat about to be launched (🎯T709).
//
// Claude Code asks that question of a human, once per directory, before
// it will draw a composer. A fleet seat has nobody to answer it: the
// launch handshake times out, the seat is reaped, and the owner sees only
// reaped_held on send — what happened to ge-po on 2026-09-20 on a
// freshly-cloned workdir.
//
// The answer is to have already answered. This runs before reg.Launch so
// the config the process reads at start already carries the yes.
//
// It never fails a launch. A workdir outside the owner roots is refused
// on purpose (trust is a safety boundary) and an unreadable config cannot
// be repaired blind — in both cases the launch proceeds, and if the
// modal does appear, agenterr.ClassWorkspaceTrust tells the owner what
// to do instead of calling it a stall that will be retried.
func PreflightTrustRegistry(reg *claudia.Registry, name string) {
	if reg == nil {
		return
	}
	def := reg.Def(name)
	if def == nil || seatprovider.Resolve(def.Provider).Plan != seatprovider.Claude || def.WorkDir == "" {
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

func (f *Claudia) preflightTrust(name string) {
	if f == nil || f.reg == nil {
		return
	}
	PreflightTrustRegistry(f.reg, name)
}
