// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// PlanProvider canonicalises a seat id to the plan Claudia's catalog
// ranks (claude, codex, grok, cursor). Sidecar launch ids
// (anthropic, openai-codex, xai-oauth) collapse onto those plans.
// The published claudia module does not export this; the mapping is
// the one jevons already uses for sidecar remint (🎯T866).
func PlanProvider(p claudia.Provider) claudia.Provider {
	switch claudia.Provider(strings.ToLower(strings.TrimSpace(string(p)))) {
	case claudia.ProviderClaude, "anthropic":
		return claudia.ProviderClaude
	case claudia.ProviderCodex, "openai-codex":
		return claudia.ProviderCodex
	case claudia.ProviderGrok, "xai-oauth", "xai":
		return claudia.ProviderGrok
	case claudia.ProviderCursor:
		return claudia.ProviderCursor
	default:
		return claudia.Provider(strings.ToLower(strings.TrimSpace(string(p))))
	}
}

// SubscriptionSeatProvider is the id Launch talks to for a subscription
// plan: claude → anthropic, codex → openai-codex, grok → xai-oauth.
// cursor already launches under that name. Other ids pass through.
// The published claudia module does not export this helper.
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
