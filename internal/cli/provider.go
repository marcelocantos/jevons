// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// ParseProvider maps a free-form provider name (or empty) onto a claudia
// Provider. Unknown non-empty values return ("", false).
func ParseProvider(name string) (claudia.Provider, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "claude", "anthropic":
		return claudia.ProviderClaude, true
	case "codex", "openai":
		return claudia.ProviderCodex, true
	case "grok", "xai", "x.ai":
		return claudia.ProviderGrok, true
	default:
		return "", false
	}
}

// InferProviderFromModel picks a provider when the caller only set a
// model string. Explicit provider always wins (call ParseProvider first).
// Heuristics: models starting with "grok" → Grok; "gpt-" / "codex" /
// "o3" / "o4" → Codex; otherwise Claude.
func InferProviderFromModel(model string) claudia.Provider {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case m == "":
		return claudia.ProviderClaude
	case strings.HasPrefix(m, "grok"):
		return claudia.ProviderGrok
	case strings.HasPrefix(m, "gpt-"),
		strings.HasPrefix(m, "codex"),
		strings.HasPrefix(m, "o3"),
		strings.HasPrefix(m, "o4"):
		return claudia.ProviderCodex
	default:
		return claudia.ProviderClaude
	}
}

// ResolveProvider chooses the runtime for a Task: explicit provider if
// valid, else infer from model, else Claude.
func ResolveProvider(provider, model string) (claudia.Provider, error) {
	if provider != "" {
		p, ok := ParseProvider(provider)
		if !ok {
			return "", errUnknownProvider(provider)
		}
		return p, nil
	}
	return InferProviderFromModel(model), nil
}

type unknownProviderError string

func errUnknownProvider(name string) error { return unknownProviderError(name) }

func (e unknownProviderError) Error() string {
	return "unknown provider " + string(e) + ` (want "claude", "codex", or "grok")`
}
