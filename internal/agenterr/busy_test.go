// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"fmt"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T214 J6: busy classifier is not Grok-ACP-string-only.
func TestIsPromptBusy(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("connection reset"), false},
		{fmt.Errorf("send failed: other"), false},
		// Grok ACP (historical 🎯T111.1 path).
		{fmt.Errorf("grok acp: prompt already in flight"), true},
		{fmt.Errorf("send to x: grok acp: prompt already in flight"), true},
		// Claudia Task.
		{fmt.Errorf("task abc is busy"), true},
		// Claude / generic session phrasing (when a backend surfaces busy).
		{fmt.Errorf("claude: prompt in progress"), true},
		{fmt.Errorf("session busy; try again"), true},
		{fmt.Errorf("agent turn in progress"), true},
	}
	for _, tc := range cases {
		got := agenterr.IsPromptBusy(tc.err)
		if got != tc.want {
			t.Errorf("IsPromptBusy(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
