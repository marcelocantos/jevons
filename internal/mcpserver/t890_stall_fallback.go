// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// 🎯T890 — a worker mint that splash-stalls on one provider is retried on a
// provider that is currently starting seats, before the leaf is lost.
//
// THE SPECIMEN. Four workers (jv-t842-stale-plan-usage,
// jv-t862.17-deny-fail-closed, jv-t857-codex-first-turn,
// jv-t562.2-visible-queue) all splash-stalled on provider=claude on
// 2026-09-28 17:17-17:23 under host load ~48, were released by 🎯T729, and
// had to be manually re-minted on provider=anthropic by the PO before they
// succeeded — even though the anthropic sidecar route was working the whole
// time and Claude weekly capacity (87%) went unspent. 🎯T729 retries the
// SAME provider (a splash screen usually just needs seconds); this closes
// the gap for the stall that survives that retry: try a DIFFERENT provider
// once before reporting the leaf lost.
//
// Distinct from 🎯T891 (prevents the false-positive stall classification in
// the first place). This target's job is the fallback AFTER a stall — real
// or false-positive — has already been classified startBriefNeverReady.

// fallbackProviderOrder is the preference ladder tried after the stalled
// provider. anthropic first: it is claudia's sidecar route onto the same
// Claude weekly capacity a stalled provider=claude mint was trying to
// spend, so a stall that is really "this CLI wedged" rather than "Claude is
// down" recovers onto capacity that was never actually unavailable.
var fallbackProviderOrder = []string{"anthropic", "grok", "codex"}

// pickStallFallbackProvider is the pure fork: given the provider that just
// splash-stalled and a predicate for "is this provider currently eligible
// to mint" (🎯T693 published-band / dest-saturation check already wired at
// the call site), return the first eligible candidate that is not the
// stalled provider — or "" when none is eligible. Never returns stalled
// itself: retrying the identical provider is 🎯T729's job, not this one's.
func pickStallFallbackProvider(stalled string, eligible func(provider string) bool) string {
	stalled = strings.ToLower(strings.TrimSpace(stalled))
	if eligible == nil {
		return ""
	}
	for _, cand := range fallbackProviderOrder {
		if cand == stalled {
			continue
		}
		if eligible(cand) {
			return cand
		}
	}
	return ""
}

// attemptStallFallbackMint retries a fresh mint on the first eligible
// fallback provider (🎯T890) after a splash-stall on the provider the
// caller asked for. Returns the fallback provider tried ("" = no eligible
// fallback, nothing attempted) and the error from that attempt (nil =
// the fallback mint succeeded and the seat is briefed).
//
// This reuses the same stitch/launch/deliver sequence jevons_agent_start
// itself just ran — the row already exists (this call minted it), so the
// only change is def.Provider and a second Launch onto that provider.
func (s *Server) attemptStallFallbackMint(
	ctx context.Context,
	name, workdir, model, taskTypeArg, parent, purpose, targetID, prompt, stalledProvider string,
) (fallbackProvider string, attemptErr error) {
	if s == nil || s.registry == nil {
		return "", nil
	}
	fallbackProvider = pickStallFallbackProvider(stalledProvider, s.providerDestEligible)
	if fallbackProvider == "" {
		return "", nil
	}
	slog.Warn("opening brief stalled; trying a fallback provider before losing the leaf",
		"component", compAgentLifecycle, "name", name,
		"stalled_provider", stalledProvider, "fallback_provider", fallbackProvider)
	s.mu.Lock()
	s.pendingSpawnRole = ""
	s.pendingOwnerAsked = false
	s.mu.Unlock()
	if _, _, _, err := s.stitchAgentStart(name, workdir, model, fallbackProvider, taskTypeArg, parent, purpose, targetID, prompt); err != nil {
		return fallbackProvider, fmt.Errorf("re-stitch onto %s: %w", fallbackProvider, err)
	}
	s.startMu.Lock()
	proc, err := s.launchAgentBounded(ctx, name)
	if err != nil {
		s.startMu.Unlock()
		return fallbackProvider, fmt.Errorf("launch on %s: %w", fallbackProvider, err)
	}
	s.wireAgentEvents(name, proc)
	s.startMu.Unlock()
	s.noteSeatMinted(name)
	if err := s.deliverStartPrompt(name, prompt); err != nil {
		return fallbackProvider, err
	}
	return fallbackProvider, nil
}
