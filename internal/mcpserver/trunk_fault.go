// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// tickTrunkFault belongs to the existing sentinel, not a second supervisor.
// Owner visibility is independent of provider capacity or successful PO wake.
func (s *Server) tickTrunkFault(workdir, po string, paused bool) {
	if workdir == "" {
		return
	}
	err := worktree.DeliverTrunkFault(workdir, func(f *worktree.TrunkFault) {
		owner, woke := false, false
		if !f.OwnerNotified && s.ownerNotifier != nil {
			owner = s.ownerNotifier.NotifyOwner("integration", worktree.TrunkRed, "⚠️ Coding work cannot land because the integration branch is already failing. "+f.Error())
		}
		if !f.POWoken && !paused && po != "" {
			brief := fmt.Sprintf("[daemon fault %s id=%s] %s\nStop retrying unrelated landings or adding ledger notes. Under the product owner, create or reuse exactly one fixer named jv-trunk-fix-%s for this incident. Repair the failing test(s), obtain a clean gate, then resume landings. No release or push. Report a concrete blocker if repair cannot start.", worktree.TrunkRed, f.ID, f.Error(), f.ID[:12])
			// Use the targeted durable send path. Do not fall back to the overseer and
			// pretend the PO received the repair mission.
			_, sendErr := s.sendToAgent(po, brief, false)
			if sendErr == nil {
				woke = true
			} else {
				slog.Warn("sentinel trunk fault PO wake", "po", po, "err", sendErr)
			}
		}
		f.OwnerNotified = f.OwnerNotified || owner
		f.POWoken = f.POWoken || woke

		s.logLifecycle(compSentinel, worktree.TrunkRed, "error", map[string]any{"id": f.ID, "tests": strings.Join(f.Tests, ", "), "owner_notified": owner || f.OwnerNotified, "po_woken": woke || f.POWoken})
	})
	if err != nil {
		slog.Error("sentinel trunk fault delivery", "err", err)
	}
}
