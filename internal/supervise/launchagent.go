// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"bytes"
	"strings"
)

// LaunchAgentInteractiveProcessType is the macOS launchd ProcessType that
// keeps supervisord and its supervised children at user-interactive
// scheduling priority (🎯T779).
const LaunchAgentInteractiveProcessType = "Interactive"

// PlistDeclaresProcessType reports whether plist bytes declare processType.
func PlistDeclaresProcessType(plist []byte, processType string) bool {
	processType = strings.TrimSpace(processType)
	if processType == "" {
		return false
	}
	body := string(plist)
	key := "<key>ProcessType</key>"
	i := strings.Index(body, key)
	if i < 0 {
		return false
	}
	rest := body[i+len(key):]
	valKey := "<string>"
	j := strings.Index(rest, valKey)
	if j < 0 {
		return false
	}
	rest = rest[j+len(valKey):]
	k := strings.Index(rest, "</string>")
	if k < 0 {
		return false
	}
	return strings.TrimSpace(rest[:k]) == processType
}

// InteractiveSupervisorLaunchAgentPlist is the minimum shape 🎯T779 requires.
func InteractiveSupervisorLaunchAgentPlist(plist []byte) bool {
	if !PlistDeclaresProcessType(plist, LaunchAgentInteractiveProcessType) {
		return false
	}
	return bytes.Contains(plist, []byte("homebrew.mxcl.supervisor")) &&
		bytes.Contains(plist, []byte("supervisord"))
}
