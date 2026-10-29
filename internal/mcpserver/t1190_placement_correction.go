// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// placementCorrectionCooldown is the least time between two placement
// notices to one author.
const placementCorrectionCooldown = time.Hour

// tryDeliverPlacementCorrection tells the author, at most once per
// placementCorrectionCooldown, when a jevons fence is present but not at an
// accepted placement (🎯T1190).
// Returns true when a notice was delivered this call (reap should be deferred).
func (s *Server) tryDeliverPlacementCorrection(name, text string) bool {
	if s == nil {
		return false
	}
	notice := envelope.PlacementCorrectionNotice(text)
	if notice == "" {
		return false
	}
	name = strings.TrimSpace(name)
	now := s.deliveryNow()
	s.mu.Lock()
	if last, ok := s.placementCorrectionAt[name]; ok && now.Sub(last) < placementCorrectionCooldown {
		s.mu.Unlock()
		return false
	}
	if s.placementCorrectionAt == nil {
		s.placementCorrectionAt = map[string]time.Time{}
	}
	s.placementCorrectionAt[name] = now
	s.mu.Unlock()
	reason, _ := envelope.PlacementRejectReason(text)
	if _, err := s.deliverByName(name, notice, OriginAgent, false); err != nil {
		slog.Warn("T1190 placement correction undeliverable", "agent", name, "err", err)
	}
	fields := map[string]any{"agent": name, "placement_reason": reason.String()}
	s.logLifecycle(compAgentLifecycle, "envelope", "placement_correction_sent", fields)
	return true
}
