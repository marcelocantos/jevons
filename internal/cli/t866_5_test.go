// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestT8665GrokAndCursorLaunchIds(t *testing.T) {
	if got := SidecarLaunchProvider(claudia.ProviderGrok); got != claudia.Provider("xai-oauth") {
		t.Fatalf("grok Launch must talk to the sidecar as xai-oauth, got %q", got)
	}
	if got := SidecarLaunchProvider(claudia.ProviderCursor); got != claudia.ProviderCursor {
		t.Fatalf("cursor Launch must talk to the sidecar as cursor, got %q", got)
	}
	if SidecarLaunchProvider(claudia.ProviderBedrock) != claudia.ProviderBedrock {
		t.Fatal("bedrock is not a subscription sidecar rewrite")
	}
}
