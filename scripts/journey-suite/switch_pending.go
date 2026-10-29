// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"
	"time"
)

// switchJoinTimeout bounds how long a detached switch (🎯T790) may take before
// the journey calls it failed. Compaction on a long conversation can run minutes.
const switchJoinTimeout = 3 * time.Minute

// isSwitchPending reports whether err is the 🎯T790 pending result of
// jevons_agent_switch_model: the move is still running in the daemon past the
// call's deadline. A genuine switch refusal is not pending.
func isSwitchPending(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "T790") {
		return true
	}
	// MCPToolCall prefixes with the tool name; match the early phrase too.
	return strings.Contains(msg, "jevons_agent_switch_model") &&
		strings.Contains(msg, "still running past this call's deadline") &&
		strings.Contains(msg, "CONTINUES in the daemon")
}

// switchModelUntilSettled calls jevons_agent_switch_model with the same args
// until the move settles or switchJoinTimeout passes (🎯T833 / 🎯T625.8 family).
// While in flight the daemon answers T790; repeating the exact call joins the
// flight or reads the recorded outcome — it never starts a second switch.
func (s *suite) switchModelUntilSettled(args map[string]any) (string, error) {
	deadline := time.Now().Add(switchJoinTimeout)
	var lastOut string
	var lastErr error
	for time.Now().Before(deadline) {
		out, err := s.mcpText("jevons_agent_switch_model", args)
		if err == nil {
			return out, nil
		}
		if !isSwitchPending(err) {
			return out, err
		}
		lastOut, lastErr = out, err
		time.Sleep(switchSettleRetry)
	}
	if lastErr != nil {
		return lastOut, fmt.Errorf("switch never settled within %s: %w", switchJoinTimeout, lastErr)
	}
	return lastOut, fmt.Errorf("switch never settled within %s", switchJoinTimeout)
}
