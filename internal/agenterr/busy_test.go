// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

type causeStub struct {
	cause string
	text  string
}

func (e causeStub) Error() string         { return e.text }
func (e causeStub) ProviderCause() string { return e.cause }

// 🎯T214 J6: busy classifier is not Grok-ACP-string-only.
func TestIsPromptBusy(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("connection reset"), false},
		{fmt.Errorf("send failed: other"), false},
		{fmt.Errorf("grok acp: %w", claudia.ErrTurnInFlight), true},
		{fmt.Errorf("send to x: %w", fmt.Errorf("grok acp: %w", claudia.ErrTurnInFlight)), true},
		{errors.New("grok acp: prompt already in flight"), false},
		{errors.New("broker protocol: agent_failed: cursor acp: prompt already in flight"), false},
		{errors.New("401 unauthorized"), false},
		{causeStub{cause: agenterr.CauseTurnInFlight, text: "401 unauthorized"}, true},
		{causeStub{cause: agenterr.CauseBusy, text: "task abc is busy"}, true},
		{causeStub{cause: agenterr.CauseProcessNotRunning, text: "claude process not running"}, false},
		{errors.New("task abc is busy"), false},
	}
	for _, tc := range cases {
		got := agenterr.IsPromptBusy(tc.err)
		if got != tc.want {
			t.Errorf("IsPromptBusy(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
