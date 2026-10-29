// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

func TestT782ProviderTransportTurnDeathSpecimens(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"Error: RetriableError: Connection stalled",
		"[canceled] http/2 stream closed",
		"Error: RetriableError: Connection stalled\n",
	} {
		if !agenterr.IsProviderTransportTurnDeath(s) {
			t.Fatalf("specimen must match: %q", s)
		}
	}
}

func TestT782ProviderTransportTurnDeathRejectsAuthoredQuote(t *testing.T) {
	t.Parallel()
	quoted := `The turn died with Error: RetriableError: Connection stalled but we retried.`
	if agenterr.IsProviderTransportTurnDeath(quoted) {
		t.Fatal("prose quoting the specimen must not match")
	}
	if agenterr.IsProviderTransportTurnDeath("Internal error") {
		t.Fatal("unrelated provider sentence must not match")
	}
}
