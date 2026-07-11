// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestProviderIsGrokOnly(t *testing.T) {
	if Provider != claudia.ProviderGrok {
		t.Fatalf("Provider = %q, want %q", Provider, claudia.ProviderGrok)
	}
}
