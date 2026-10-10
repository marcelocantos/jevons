// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestOMPSeatProviderOnlyNamesVerifiedSessionTransports(t *testing.T) {
	for _, tc := range []struct{ in, want claudia.Provider }{
		{"claude", "anthropic"}, {"anthropic", "anthropic"},
		{"codex", "openai-codex"}, {"openai-codex", "openai-codex"},
		{"grok", "xai-oauth"}, {"xai", "xai-oauth"}, {"xai-oauth", "xai-oauth"},
		{"cursor", "cursor"}, {"  CLAUDE  ", "anthropic"},
	} {
		got, err := OMPSeatProvider(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("OMPSeatProvider(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestOMPSeatProviderRefusesUnknownAndTaskOnlyWithoutFallback(t *testing.T) {
	for _, p := range []claudia.Provider{"", "bedrock", "ollama", "future-provider"} {
		got, err := OMPSeatProvider(p)
		if got != "" || err == nil || !strings.Contains(err.Error(), "refusing CLI fallback") {
			t.Errorf("OMPSeatProvider(%q) = %q, %v; want explicit refusal", p, got, err)
		}
	}
}
