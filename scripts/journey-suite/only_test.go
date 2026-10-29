// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestJourneyOnlyExactIDDoesNotSelectNumberedSiblings(t *testing.T) {
	for _, tc := range []struct {
		name, only string
		want       bool
	}{
		{"J3-cancel-and-send", "J3", true},
		{"J30-packaged-react-owner-turn", "J3", false},
		{"J32-overseer-overlap-queue", "J3", false},
		{"J39-t952-hold", "J3", false},
		{"J30-packaged-react-owner-turn", "J30,J3", true},
		{"J3-cancel-and-send", "J30,J3", true},
		{"J3-cancel-and-send", "cancel-and-send", true},
		{"J3-cancel-and-send", "J3-", true},
	} {
		if got := journeyNameMatches(tc.name, tc.only); got != tc.want {
			t.Errorf("journeyNameMatches(%q, %q) = %v, want %v", tc.name, tc.only, got, tc.want)
		}
	}
}
