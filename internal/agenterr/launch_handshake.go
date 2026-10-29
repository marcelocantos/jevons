// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// readyTimeoutMarker is claudia's historical ready-timeout wording; the
// frame follows. Named reasons use "claude not ready (token)".
const readyTimeoutMarker = "ready pattern did not match within"

func isReadyTimeoutMessage(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, readyTimeoutMarker) ||
		strings.Contains(low, "claude not ready (") ||
		strings.Contains(low, "still /rc connecting") ||
		strings.Contains(low, "startup settings warnings")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ClassStartupStall: a launch handshake timed out with startup chrome on
// screen but no composer (🎯T565). Historical class — omp seats rarely
// hit it, but workspace-trust negatives and recover policy still name it.
const ClassStartupStall Class = "startup_stall"

var settingsWarningMarkers = []string{
	"permission deny rule",
	"permission allow rule",
	"matches no known tool",
	"settings.json",
}

var ownerCopyStartupStallMarkers = []string{
	"agent cli stalled on startup",
	"agent cli stalled on remote-control handshake",
}

func namedReadyReason(msg string) string {
	low := strings.ToLower(msg)
	for _, token := range []string{"rc_connecting", "settings_warning", "splash", "no_composer"} {
		if strings.Contains(low, "claude not ready ("+token+")") ||
			strings.Contains(low, "reason="+token) ||
			strings.Contains(low, "startup_stall / "+token) {
			return token
		}
	}
	if strings.Contains(low, "/rc connecting") {
		return "rc_connecting"
	}
	return ""
}

// IsStartupStall reports a ready-timeout whose frame is startup output, not
// workspace trust (🎯T565 / historical tmux path).
func IsStartupStall(msg string) bool {
	if IsWorkspaceTrust(msg) {
		return false
	}
	low := strings.ToLower(msg)
	if containsAny(low, ownerCopyStartupStallMarkers...) {
		return true
	}
	switch namedReadyReason(msg) {
	case "settings_warning", "rc_connecting", "splash", "no_composer":
		return true
	}
	if strings.Contains(low, "startup settings warnings") {
		return true
	}
	if !strings.Contains(low, readyTimeoutMarker) {
		return false
	}
	frame := strings.ToLower(frameOf(msg))
	if containsAny(frame, settingsWarningMarkers...) {
		return true
	}
	return false
}

// classifyLaunchHandshakeText maps ready-timeout prose to structured
// classes when the error carries no typed provider cause.
func classifyLaunchHandshakeText(msg string) Class {
	if IsWorkspaceTrust(msg) {
		return ClassWorkspaceTrust
	}
	if IsStartupStall(msg) {
		return ClassStartupStall
	}
	return ClassNone
}

func startupStallCopy(raw string) string {
	return "Agent CLI stalled on startup (startup_stall). The process is alive but has not finished drawing a composer — re-pressure may help once the pane clears. " +
		detailSuffix(raw)
}
