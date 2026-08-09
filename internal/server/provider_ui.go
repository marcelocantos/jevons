// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/marcelocantos/jevons/internal/ui"
)

// Server-driven provider UI producer (🎯T27.6): composes every attached
// provider's declared ViewNode surface into one tree (internal/ui) and
// pushes it to T9/T11 clients on the /ws/remote lane as a standard
// ViewMessage — seeded on client init, re-broadcast reactively when the
// FeedHub reports a UI-relevant change (attach, or an event on a feed a
// surface binds). Client actions inside the composed tree carry the
// "provider/{id}/{surface}/{action}" namespace and are relayed back to
// the owning provider over its feed socket.

// providerViewMessage composes the current provider view. ok is false
// when there is nothing to show (no hub or no surfaces with roots).
func (s *Server) providerViewMessage() (ui.ViewMessage, bool) {
	s.mu.RLock()
	hub := s.providerFeeds
	s.mu.RUnlock()
	if hub == nil {
		return ui.ViewMessage{}, false
	}
	root := ui.Compose(hub.ComposedUI())
	if len(root.Children) == 0 {
		return ui.ViewMessage{}, false
	}
	return ui.ViewMessage{Type: "view", Root: root, Slot: ui.SlotProviders}, true
}

// BroadcastProviderView recomposes and pushes the provider view to all
// connected clients. Wired to FeedHub.OnUI in production.
func (s *Server) BroadcastProviderView() {
	if msg, ok := s.providerViewMessage(); ok {
		s.Broadcast(msg)
	}
}

// relayProviderAction routes a namespaced client action back to the
// provider that owns the surface. Returns false for actions outside the
// provider namespace so the caller falls through to its own handling.
func (s *Server) relayProviderAction(action, value string) bool {
	pid, surface, name, ok := ui.ParseAction(action)
	if !ok {
		return false
	}
	s.mu.RLock()
	hub := s.providerFeeds
	s.mu.RUnlock()
	if hub == nil {
		slog.Warn("provider action with no feed hub", "action", action)
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hub.SendAction(ctx, pid, surface, name, value); err != nil {
		slog.Warn("provider action relay failed", "provider", pid, "surface", surface, "action", name, "err", err)
	}
	return true
}
