// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// ClassStartupStall: a launch handshake timed out with the CLI's startup
// output on screen but no composer (🎯T565). On 2026-08-29 the last frame
// was two `Permission deny rule "TeamCreate" matches no known tool` lines
// over an otherwise blank pane: claude printed its settings warnings and
// never drew the input box within the ready timeout. That was logged as
// failure_class=none and the seat was retired unbriefed (🎯T433). It is
// neither a cloud outage nor a wire bug: the process is alive and has not
// finished starting — re-pressure-worthy, and the owner is told what the
// pane actually showed.
const ClassStartupStall Class = "startup_stall"

// readyTimeoutMarker is claudia's historical ready-timeout wording; the
// frame follows. Named reasons (🎯T565 part 3a) use "claude not ready (token)".
const readyTimeoutMarker = "ready pattern did not match within"

// settingsWarningMarkers are the startup notices Claude Code prints to the
// terminal before its TUI mounts. A frame made only of these is startup
// output, not a composer that failed to match.
var settingsWarningMarkers = []string{
	"permission deny rule",
	"permission allow rule",
	"matches no known tool",
	"settings.json",
}

func isReadyTimeoutMessage(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, readyTimeoutMarker) ||
		strings.Contains(low, "claude not ready (") ||
		strings.Contains(low, "still /rc connecting") ||
		strings.Contains(low, "startup settings warnings") ||
		strings.Contains(low, "startup_stall")
}

func namedReadyReason(msg string) string {
	// 🎯T709: a no_composer stall whose last frame is the workspace-trust
	// dialog is not a generic ready miss — classify it before the token
	// list so OwnerCopy can name the recoverable action. The dialog itself
	// is Claudia WaitReady (🎯T87); this token is the host-side class.
	if IsWorkspaceTrust(msg) {
		return "workspace_trust"
	}
	low := strings.ToLower(msg)
	for _, token := range []string{"rc_connecting", "settings_warning", "splash", "no_composer", "workspace_trust"} {
		if strings.Contains(low, "claude not ready ("+token+")") || strings.Contains(low, "reason="+token) {
			return token
		}
	}
	if strings.Contains(low, "/rc connecting") {
		return "rc_connecting"
	}
	return ""
}

// workspaceTrustMarkers are Claude Code's TUI / warning copy for the
// workspace-trust dialog (Colossus ge-po 2026-09-20, 🎯T709).
var workspaceTrustMarkers = []string{
	"quick safety check",
	"is this a project you created or one you trust",
	"yes, i trust this folder",
	"i trust this folder",
	"this workspace has not been trusted",
	"workspace has not been trusted",
}

// IsWorkspaceTrust reports whether msg is a ready stall whose pane is
// Claude's workspace-trust dialog (or the same warning after formatting).
func IsWorkspaceTrust(msg string) bool {
	low := strings.ToLower(msg)
	for _, m := range workspaceTrustMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// LastFrame returns the pane text a ready-timeout error carried after
// "last frame:", trimmed, or "" when the error is not a ready timeout.
func LastFrame(msg string) string {
	if !isReadyTimeoutMessage(msg) && !IsWorkspaceTrust(msg) {
		return ""
	}
	low := strings.ToLower(msg)
	idx := strings.Index(low, "last frame:")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(msg[idx+len("last frame:"):])
}

// IsStartupStall reports whether msg is a ready timeout whose last frame is
// startup output only — settings warnings (or an empty pane) with no
// composer glyph anywhere on screen — or a named claudia stall
// (settings_warning / rc_connecting / splash / no_composer). A generic
// pattern-mismatch whose last frame already shows the ❯ prompt is not a
// stall.
func IsStartupStall(msg string) bool {
	low := strings.ToLower(msg)
	switch namedReadyReason(msg) {
	case "settings_warning", "rc_connecting", "splash", "no_composer", "workspace_trust":
		return true
	}
	if strings.Contains(low, "startup settings warnings") {
		return true
	}
	if !strings.Contains(low, readyTimeoutMarker) {
		return false
	}
	_, after, ok := strings.Cut(low, "last frame:")
	if !ok || strings.Contains(after, "❯") {
		return false
	}
	for _, line := range strings.Split(after, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !containsAny(line, settingsWarningMarkers...) {
			return false
		}
	}
	return true
}
