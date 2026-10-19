// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1079: the plan login a Reauth repairs and the badge company a model is
// checked against do not depend on which spelling the row stores. The old
// tables named launch ids for one and plan names for the other, so a codex
// row was never offered Reauth and an xai-oauth row had no company.
func TestT1079LegacyPlanRowMatchesLaunchRow(t *testing.T) {
	for _, pair := range [][2]claudia.Provider{
		{"grok", "xai-oauth"}, {"claude", "anthropic"}, {"codex", "openai-codex"},
	} {
		legacy, launch := pair[0], pair[1]
		if reauthablePlanProvider(legacy) != reauthablePlanProvider(launch) || !reauthablePlanProvider(launch) {
			t.Errorf("reauth: %s=%v %s=%v, want both true", legacy, reauthablePlanProvider(legacy), launch, reauthablePlanProvider(launch))
		}
		if recoverableDestinationProvider(legacy) != recoverableDestinationProvider(launch) {
			t.Errorf("recover destination: %s=%q %s=%q", legacy, recoverableDestinationProvider(legacy), launch, recoverableDestinationProvider(launch))
		}
		if a, b := providerCompany(string(legacy)), providerCompany(string(launch)); a != b || a == "" {
			t.Errorf("company: %s=%q %s=%q", legacy, a, launch, b)
		}
		// A foreign model is refused on both spellings.
		if modelFitsProvider(string(legacy), "gpt-5.5") != modelFitsProvider(string(launch), "gpt-5.5") {
			t.Errorf("model fit differs for %s and %s", legacy, launch)
		}
	}
}
