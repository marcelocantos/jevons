// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatprovider"
)

func TestOverseerSwitchDestinationClaudeToGrok(t *testing.T) {
	got := overseerSwitchDestination(claudia.ProviderClaude)
	if seatprovider.Resolve(got).Plan != seatprovider.Grok {
		t.Fatalf("claude overseer switch = %s, want grok plan", got)
	}
}

func TestOverseerSwitchDestinationGrokToClaude(t *testing.T) {
	got := overseerSwitchDestination(claudia.ProviderGrok)
	if seatprovider.Resolve(got).Plan != seatprovider.Claude {
		t.Fatalf("grok overseer switch = %s, want claude plan", got)
	}
}

func TestWorkerSwitchDestinationStillCodexPairing(t *testing.T) {
	got := switchJourneyDestination(claudia.ProviderClaude)
	if seatprovider.Resolve(got).Plan != seatprovider.Codex {
		t.Fatalf("worker switch from claude = %s, want codex", got)
	}
}
