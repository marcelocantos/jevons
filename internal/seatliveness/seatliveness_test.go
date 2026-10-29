// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatliveness

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/seatstate"
)

func TestServed(t *testing.T) {
	for _, tc := range []struct {
		alive seatstate.Tri
		want  string
	}{
		{seatstate.Yes, ServedLive},
		{seatstate.No, ServedDead},
		{seatstate.Unknown, ServedUnknown},
	} {
		if got := Served(seatstate.State{Alive: tc.alive}); got != tc.want {
			t.Fatalf("alive=%s: got %q want %q", tc.alive, got, tc.want)
		}
	}
}
