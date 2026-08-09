// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestDefaultModelForProvider(t *testing.T) {
	if got := DefaultModelForProvider(claudia.ProviderGrok); got != DefaultGrokModel {
		t.Fatalf("grok default=%q want %q", got, DefaultGrokModel)
	}
	if got := DefaultModelForProvider("xai"); got != DefaultGrokModel {
		t.Fatalf("xai default=%q want %q", got, DefaultGrokModel)
	}
	if got := DefaultModelForProvider(claudia.ProviderClaude); got != "" {
		t.Fatalf("claude default=%q want empty (session log / pin)", got)
	}
	if got := DefaultModelForProvider(""); got != "" {
		t.Fatalf("empty provider default=%q want empty", got)
	}
}

func TestBindSessionModel(t *testing.T) {
	if got := BindSessionModel("fable", claudia.ProviderClaude); got != "fable" {
		t.Fatalf("pin wins: %q", got)
	}
	if got := BindSessionModel("", claudia.ProviderGrok); got != DefaultGrokModel {
		t.Fatalf("empty pin on grok: %q want %q", got, DefaultGrokModel)
	}
	if got := BindSessionModel("  ", claudia.ProviderGrok); got != DefaultGrokModel {
		t.Fatalf("whitespace pin on grok: %q want %q", got, DefaultGrokModel)
	}
	if got := BindSessionModel("grok-4.5-build", claudia.ProviderGrok); got != "grok-4.5-build" {
		t.Fatalf("explicit grok pin: %q", got)
	}
	if got := BindSessionModel("", claudia.ProviderClaude); got != "" {
		t.Fatalf("empty pin on claude: %q want empty", got)
	}
}
