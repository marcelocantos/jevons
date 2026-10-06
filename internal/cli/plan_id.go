// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// PlanProvider canonicalises a seat id to the plan Claudia's catalog
// ranks (claude, codex, grok, cursor). Sidecar launch ids
// (anthropic, openai-codex, xai-oauth) collapse onto those plans via
// claudia.PlanProvider (🎯T1008); Jevons layers case-insensitivity and
// the bare "xai" alias on top, which the published function does not.
func PlanProvider(p claudia.Provider) claudia.Provider {
	norm := claudia.Provider(strings.ToLower(strings.TrimSpace(string(p))))
	if norm == "xai" {
		norm = "xai-oauth"
	}
	return claudia.PlanProvider(norm)
}

// SubscriptionSeatProvider is the id Launch talks to for a subscription
// plan: claude → anthropic, codex → openai-codex, grok → xai-oauth.
// cursor already launches under that name. Other ids pass through.
// Delegates to claudia.SubscriptionSeatProvider (🎯T1008) once the id
// is canonicalised through PlanProvider above.
func SubscriptionSeatProvider(p claudia.Provider) claudia.Provider {
	switch PlanProvider(p) {
	case claudia.ProviderClaude:
		return "anthropic"
	case claudia.ProviderCodex:
		return "openai-codex"
	case claudia.ProviderGrok:
		return "xai-oauth"
	case claudia.ProviderCursor:
		return claudia.ProviderCursor
	default:
		return claudia.Provider(strings.TrimSpace(string(p)))
	}
}
