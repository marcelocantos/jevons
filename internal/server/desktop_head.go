// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"

	"github.com/marcelocantos/jevons/internal/desktop"
	"github.com/marcelocantos/jevons/internal/ui"
)

// Desktop head HTTP surface (🎯T27.7): the server-side projection of the
// menu-bar/tray model so hermetics and live probes can assert section
// membership without launching AppKit. The real head binary also
// consumes /ws/remote view frames; this endpoint is the snapshot twin.

// handleDesktopHead returns the current menu-bar model: one section per
// enabled provider with a composed UI surface, plus the mnemo
// supersession inventory.
func (s *Server) handleDesktopHead(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	hub := s.providerFeeds
	s.mu.RUnlock()

	var sections []desktop.Section
	var childCount int
	if hub != nil {
		surfaces := hub.ComposedUI()
		root := ui.Compose(surfaces)
		childCount = len(root.Children)
		sections = desktop.SectionsFromSurfaces(surfaces)
	}
	if sections == nil {
		sections = []desktop.Section{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"connected":            true, // server is the source; head is a client
		"sections":             sections,
		"section_count":        len(sections),
		"compose_child_count":  childCount,
		"supersedes":           desktop.SupersededMnemoTargets,
		"inventory":            desktop.MnemoSupersessionInventory(),
		"slot":                 ui.SlotProviders,
	})
}
