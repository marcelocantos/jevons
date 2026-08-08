// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// Provider-default model ids for session-truth badge binding (🎯T324).
//
// Launch binds provider+model for the current SessionID: an explicit pin
// wins; otherwise the backend's documented default is written so cold
// agents (Grok ACP names no model on the wire until turn_completed) never
// sit forever on a mark-only badge. These tokens are condensable by the
// web helper (grok-4.5 → "4.5"); session logs may later refine to a
// billing id (grok-4.5-build) with the same condensed label.
//
// Empty means "no known default" — Claude often reports message.model on
// the first assistant frame, so leaving the pin empty is fine until a
// session log or observation fills it.

// DefaultGrokModel is the Grok CLI default for new sessions (see
// ~/.grok config [models].default / user guide). Condenses to "4.5".
const DefaultGrokModel = "grok-4.5"

// DefaultModelForProvider returns the condensable model id bound when an
// agent is launched on provider with an empty pin. Empty when the
// backend has no standing default we should invent.
func DefaultModelForProvider(provider claudia.Provider) string {
	switch claudia.Provider(strings.ToLower(strings.TrimSpace(string(provider)))) {
	case claudia.ProviderGrok, "xai":
		return DefaultGrokModel
	default:
		return ""
	}
}

// BindSessionModel resolves the session-truth model for a Launch /
// migrate / rehydrate binding: non-empty pin wins, else the provider
// default. Never invents a cross-provider id — the pin is the caller's.
func BindSessionModel(pin string, provider claudia.Provider) string {
	if p := strings.TrimSpace(pin); p != "" {
		return p
	}
	return DefaultModelForProvider(provider)
}
