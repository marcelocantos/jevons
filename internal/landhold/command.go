// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package landhold

import "strings"

// CommandAction reports whether a shell command appears to run the integrator
// or the development daemon restart script (🎯T1040 / T1037 incident shape).
func CommandAction(command string) (Action, bool) {
	c := strings.ToLower(command)
	if strings.Contains(c, "cmd/integrate") || (strings.Contains(c, "go run") && strings.Contains(c, "integrate")) {
		return ActionIntegrate, true
	}
	if strings.Contains(c, "restart-jevonsd.sh") {
		return ActionRestart, true
	}
	return 0, false
}
