// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T1230: a typed rate_limit (Cursor Connect error resource_exhausted as
// claudia v0.66.0 emits Reason=rate_limit) rides the existing recover
// ladder. The owner is told only when consecutive strikes reach the
// persistence threshold — not on the first refusal.

const rateLimitPersistedKind = "rate_limit_persisted"

// FormatRateLimitPersistedNotice is the owner-facing text when a seat's
// typed rate_limit has persisted past FallbackAfterStrikes.
func FormatRateLimitPersistedNotice(name string) string {
	return fmt.Sprintf(
		"Provider rate limit (rate_limit) persisted past the backoff ladder on %s after %d consecutive refusals. "+
			"The seat is backing off rather than resubmitting into the wall. "+
			"A model fallback runs if this provider has a next rung; otherwise recover waits out the ladder.",
		name, FallbackAfterStrikes)
}

// maybeNotifyRateLimitPersisted fires once when consecutive typed
// rate_limit terminals first reach FallbackAfterStrikes. Earlier refusals
// stay off the owner journal (🎯T1230).
func (s *Server) maybeNotifyRateLimitPersisted(name string, class agenterr.Class) {
	if s == nil || class != agenterr.ClassRateLimit || name == "" {
		return
	}
	s.mu.Lock()
	tracker := s.idleActivity
	n := s.ownerNotifier
	s.mu.Unlock()
	if tracker == nil {
		return
	}
	if tracker.Get(name).RateLimitStrikes != FallbackAfterStrikes {
		return
	}
	text := FormatRateLimitPersistedNotice(name)
	if n == nil {
		slog.Warn("rate_limit persisted past ladder with no owner notifier wired",
			"agent", name, "strikes", FallbackAfterStrikes)
		return
	}
	n.NotifyOwner(name, rateLimitPersistedKind, text)
}
