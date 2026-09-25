// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"strings"

	"github.com/marcelocantos/claudia"
)

// EnvProvider is the environment variable for the daemon-wide default
// agent backend when config.yaml omits provider (🎯T148).
const EnvProvider = "JEVONS_PROVIDER"

// DefaultProvider is the compile-time fallback when config and env are
// both empty. Historical Grok-only posture; empty config keeps Grok.
const DefaultProvider = claudia.ProviderGrok

// Provider is the historical compile-time default (Grok). Prefer
// ResolveProvider / SelectAgentProvider for new code; this alias remains
// so older call sites and tests that mean "the built-in fallback" stay
// readable. It is NOT a force-all-agents constant.
const Provider = DefaultProvider

// ResolveProvider picks a claudia provider id for new agents/tasks.
//
// Precedence (🎯T148):
//  1. non-empty override (per-spawn ad hoc)
//  2. non-empty cfgProvider (config.yaml provider field)
//  3. JEVONS_PROVIDER env
//  4. DefaultProvider (grok)
//
// Provider strings are passed through without an allow-list so future
// claudia backends (e.g. Bedrock) are not blocked at the selection surface.
func ResolveProvider(override, cfgProvider string) claudia.Provider {
	if p := strings.TrimSpace(override); p != "" {
		return claudia.Provider(p)
	}
	if p := strings.TrimSpace(cfgProvider); p != "" {
		return claudia.Provider(p)
	}
	if p := strings.TrimSpace(os.Getenv(EnvProvider)); p != "" {
		return claudia.Provider(p)
	}
	return DefaultProvider
}

// SelectAgentProvider chooses the provider for agent_start / resume.
//
//   - non-empty override → use it (ad hoc per start, may update registry)
//   - else non-empty stored registry provider → keep it (never clobber to Grok)
//   - else defaultProv (already resolved from config/env) → use it
//   - else DefaultProvider
func SelectAgentProvider(override string, stored, defaultProv claudia.Provider) claudia.Provider {
	if p := strings.TrimSpace(override); p != "" {
		return claudia.Provider(p)
	}
	if stored != "" {
		return stored
	}
	if defaultProv != "" {
		return defaultProv
	}
	return DefaultProvider
}

// SidecarLaunchProvider rewrites a subscription-plan fleet id onto the
// Oh My Pi provider Launch talks to (🎯T866.5 / T866.6). Claude Pro/Max
// is anthropic; ChatGPT Codex is openai-codex; grok is xai-oauth.
// cursor already launches through the sidecar under that name.
func SidecarLaunchProvider(p claudia.Provider) claudia.Provider {
	switch p {
	case claudia.ProviderClaude:
		return claudia.Provider("anthropic")
	case claudia.ProviderCodex:
		return claudia.Provider("openai-codex")
	case claudia.ProviderGrok:
		return claudia.Provider("xai-oauth")
	default:
		return p
	}
}
