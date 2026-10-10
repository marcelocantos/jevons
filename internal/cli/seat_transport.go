// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
)

// OMPSeatProvider resolves a fleet Session provider to an explicit OMP
// transport. It is deliberately distinct from ResolveProvider: the latter
// also selects providers for one-off Tasks (including Bedrock), whereas a
// persistent seat must never fall through to a CLI or an unknown backend.
// The returned provider is a runtime id, not the plan id used by Resolve.
func OMPSeatProvider(p claudia.Provider) (claudia.Provider, error) {
	id := claudia.Provider(strings.ToLower(strings.TrimSpace(string(p))))
	switch id {
	case claudia.ProviderClaude, "anthropic":
		return "anthropic", nil
	case claudia.ProviderCodex, "openai-codex":
		return "openai-codex", nil
	case claudia.ProviderGrok, "xai-oauth", "xai":
		return "xai-oauth", nil
	case claudia.ProviderCursor:
		return claudia.ProviderCursor, nil
	default:
		return "", fmt.Errorf("fleet Session provider %q has no verified OMP transport; refusing CLI fallback", p)
	}
}
