// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestResolveProvider(t *testing.T) {
	cases := []struct {
		provider, model string
		want            claudia.Provider
		wantErr         bool
	}{
		{"", "", claudia.ProviderGrok, false}, // default harness
		{"grok", "", claudia.ProviderGrok, false},
		{"claude", "", claudia.ProviderClaude, false},
		{"", "grok-4", claudia.ProviderGrok, false},
		{"", "sonnet", claudia.ProviderClaude, false},
		{"codex", "gpt-5", claudia.ProviderCodex, false},
		{"", "gpt-5.4", claudia.ProviderCodex, false},
		{"nope", "", "", true},
	}
	for _, tc := range cases {
		got, err := ResolveProvider(tc.provider, tc.model)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ResolveProvider(%q,%q) err=nil, want error", tc.provider, tc.model)
			}
			continue
		}
		if err != nil {
			t.Errorf("ResolveProvider(%q,%q): %v", tc.provider, tc.model, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ResolveProvider(%q,%q) = %q, want %q", tc.provider, tc.model, got, tc.want)
		}
	}
}
