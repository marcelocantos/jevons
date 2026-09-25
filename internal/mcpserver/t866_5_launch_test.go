// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestT8665LaunchConfigUsesSidecarIDs(t *testing.T) {
	grok := startConfigFromDef(&claudia.AgentDef{Name: "jevons", Provider: claudia.ProviderGrok})
	if grok.Provider != claudia.Provider("xai-oauth") {
		t.Fatalf("grok Launch talks to %q, want xai-oauth", grok.Provider)
	}
	cursor := startConfigFromDef(&claudia.AgentDef{Name: "c", Provider: claudia.ProviderCursor})
	if cursor.Provider != claudia.ProviderCursor {
		t.Fatalf("cursor Launch talks to %q", cursor.Provider)
	}
	claude := startConfigFromDef(&claudia.AgentDef{Name: "po", Provider: claudia.ProviderClaude})
	if claude.Provider != claudia.Provider("anthropic") {
		t.Fatalf("claude Launch talks to %q, want anthropic", claude.Provider)
	}
	if startConfigFromDef(&claudia.AgentDef{Provider: claudia.ProviderBedrock}).Provider != claudia.ProviderBedrock {
		t.Fatal("bedrock is not a subscription sidecar rewrite")
	}
}
