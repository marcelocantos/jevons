// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package upgrade

import "testing"

func TestT1047HandoffDoesNotPreserveStaleCLI(t *testing.T) {
	for _, tc := range []struct {
		name     string
		h        Handle
		pid, win bool
		want     bool
	}{
		{"live grok", Handle{Alive: true, Provider: "grok", ConnectURL: "ws://127.0.0.1:1", PID: 45}, true, false, true},
		{"dead grok", Handle{Alive: true, Provider: "grok", ConnectURL: "ws://127.0.0.1:1", PID: 45}, false, false, false},
		{"live claude", Handle{Alive: true, Provider: "claude", TmuxWindowID: "@113"}, false, true, true},
		{"stale claude", Handle{Alive: true, Provider: "claude", TmuxWindowID: "@113"}, false, false, false},
		{"stale flag with live window", Handle{Alive: false, Provider: "claude", TmuxWindowID: "@113"}, false, true, true},
		{"not tmux", Handle{Alive: true, Provider: "codex", TmuxWindowID: "codex-app-server"}, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := handoffTransportAlive(tc.h, func(int) bool { return tc.pid }, func(string) bool { return tc.win })
			if got != tc.want {
				t.Fatalf("alive=%v want %v: %+v", got, tc.want, tc.h)
			}
		})
	}
}
