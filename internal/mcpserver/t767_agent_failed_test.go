// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"fmt"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// TestT767QueueSendDefinitelyNotSentTypedRefusal pins the drain predicate
// (🎯T767): prose that quotes or renders a broker refusal is not definitely-not-
// sent; typed turn-in-flight and process-not-running causes still are.
func TestT767QueueSendDefinitelyNotSentTypedRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "quoted busy text",
			err:  errors.New("provider response quoted: grok acp: prompt already in flight"),
			want: false,
		},
		{
			name: "rendered broker refusal without typed frame",
			err:  errors.New("broker protocol: agent_failed: cursor acp: prompt already in flight"),
			want: false,
		},
		{
			name: "process gone rendered text",
			err:  errors.New("claude process not running"),
			want: false,
		},
		{
			name: "direct turn in flight sentinel",
			err:  fmt.Errorf("grok acp: %w", claudia.ErrTurnInFlight),
			want: true,
		},
		{
			name: "typed cause on broker-shaped text",
			err: &t7664Cause{
				cause: agenterr.CauseTurnInFlight,
				text:  "broker protocol: agent_failed: grok acp: prompt already in flight",
			},
			want: true,
		},
		{
			name: "typed process not running",
			err: &t7664Cause{
				cause: agenterr.CauseProcessNotRunning,
				text:  "claude process not running",
			},
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := queueSendDefinitelyNotSent(tc.err); got != tc.want {
				t.Fatalf("queueSendDefinitelyNotSent() = %v, want %v", got, tc.want)
			}
		})
	}
}
