// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"

	"github.com/marcelocantos/jevons/internal/attrib"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// tickWholeModuleGate is the 🎯T1237 standing net: integrate and worker gates
// each cover a package subset, so nothing runs the whole module against the
// shared tip. This piggybacks on the existing sentinel tick (no second
// supervisor), debounces on tip (settle), dedups per tip, and reuses the
// 🎯T1122 TrunkFault evidence/delivery path that tickTrunkFault already
// drains — a RED whole-module run surfaces exactly the way a RED post-merge
// gate does, without a second notification path to keep in sync.
func (s *Server) tickWholeModuleGate(workdir string) {
	if workdir == "" {
		return
	}
	tip, err := attrib.Git(workdir, "rev-parse", "HEAD")
	if err != nil || tip == "" {
		return
	}
	settled, err := worktree.SettleWholeModuleGateTip(workdir, tip)
	if err != nil {
		slog.Warn("sentinel whole-module gate settle", "err", err)
		return
	}
	if !settled {
		return
	}
	// Runs the heavy `go test ./...` synchronously on the sentinel's own
	// goroutine; RunWholeModuleGate's in-process mutex makes a second,
	// overlapping tick a no-op rather than queueing a pile-up behind the
	// heavy lease.
	go func() {
		rec, err := worktree.RunWholeModuleGate(workdir, gate.DefaultStoreRoot(), tip)
		if err != nil {
			slog.Warn("sentinel whole-module gate run", "err", err)
			return
		}
		if rec != nil {
			s.logLifecycle(compSentinel, "whole_module_gate", "ok", map[string]any{
				"tip": tip, "gate_id": rec.ID, "verdict": string(rec.Verdict),
			})
		}
	}()
}
