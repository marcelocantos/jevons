// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "fmt"

// ClassifyFleetMute is 🎯T418 clause 6. Recovery that needs a live
// rescuer to press Enter is not recovery. When every registered agent
// is stuck and work is queued, the daemon itself must say so.
func ClassifyFleetMute(registered, live, queued int) (mute bool, reason string) {
	if registered <= 0 {
		return false, "no registered agents"
	}
	if queued <= 0 {
		return false, "no queued work"
	}
	if live > 0 {
		return false, "at least one agent is live"
	}
	return true, fmt.Sprintf(
		"MUTE: all %d registered agent(s) are stuck and %d accepted message(s) sit queued — no rescuer is awake to press Enter",
		registered, queued)
}
