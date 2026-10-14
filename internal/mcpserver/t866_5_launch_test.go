// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestT8665LaunchConfigUsesSidecarIDs(t *testing.T) {
	grok := startConfigFromDef(&claudia.AgentDef{Name: "jevons", Provider: claudia.ProviderGrok})
	if grok.Provider != claudia.ProviderGrok {
		t.Fatalf("legacy grok CLI Launch talks to %q, want grok", grok.Provider)
	}
	cursor := startConfigFromDef(&claudia.AgentDef{Name: "c", Provider: claudia.ProviderCursor})
	if cursor.Provider != claudia.ProviderCursor {
		t.Fatalf("cursor Launch talks to %q", cursor.Provider)
	}
	claude := startConfigFromDef(&claudia.AgentDef{Name: "po", Provider: claudia.ProviderClaude})
	if claude.Provider != claudia.ProviderClaude {
		t.Fatalf("legacy claude CLI Launch talks to %q, want claude", claude.Provider)
	}
	if startConfigFromDef(&claudia.AgentDef{Provider: "anthropic"}).Provider != "anthropic" {
		t.Fatal("persisted sidecar provider changed at Launch")
	}
	if startConfigFromDef(&claudia.AgentDef{Provider: claudia.ProviderBedrock}).Provider != claudia.ProviderBedrock {
		t.Fatal("bedrock is not a subscription sidecar rewrite")
	}
}
