// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// ClassWorkspaceTrust: a launch handshake timed out with Claude Code's
// workspace-trust dialog on the pane (🎯T709).
//
// On 2026-09-20 the owner minted ge-po (and briefly yourworld2-po /
// esfera2-po) on freshly-cloned squz workdirs. Each seat took the mint,
// then vanished: POST /api/agents/<name>/send answered reaped_held, and
// the launch error underneath carried
//
//	claude not ready (no_composer): … last frame:
//	Quick safety check: Is this a project you created or one you trust…
//
// Classified as no_composer, that reads "the seat is retried" — and the
// retry is a lie. A modal waiting on a human keypress does not clear
// because a timeout fired again; claudia presses Enter at most
// maxMenuDismissals times and then gives up. Every retry burned another
// launch, and the seat was reaped with nothing owner-visible saying why.
//
// So this is its own class, and deliberately NOT transient: waiting is
// exactly the wrong response. The owner copy names the one action that
// resolves it, and 🎯T709's other half stops it arising at all by
// pre-accepting trust for owner workdirs at mint (internal/claudetrust).
const ClassWorkspaceTrust Class = "workspace_trust"

// trustDialogMarkers are phrases from Claude Code's trust prompt. Two
// generations of the dialog are covered: the older "Do you trust the
// files in this folder?" and the 2026 "Quick safety check: Is this a
// project you created or one you trust?".
//
// Each marker is a full phrase rather than a word, because this decides
// whether the owner is told to go press a key. A frame that merely says
// "trust" — an agent discussing trust in its own transcript, say — must
// not be read as a modal nobody can see.
var trustDialogMarkers = []string{
	"do you trust the files in this folder",
	"is this a project you created or one you trust",
	"quick safety check",
	"yes, i trust this folder",
	"claude code may read files in this folder",
}

// IsWorkspaceTrust reports whether msg is a ready timeout whose last
// frame shows the workspace-trust dialog.
//
// The frame is what decides it, not the named reason: claudia reports
// this as no_composer (the composer genuinely never drew) or as an
// undismissable startup menu, and both are the same owner problem.
func IsWorkspaceTrust(msg string) bool {
	if !isReadyTimeoutMessage(msg) && !isUndismissedMenu(msg) {
		return false
	}
	return containsAny(strings.ToLower(frameOf(msg)), trustDialogMarkers...)
}

// isUndismissedMenu recognises claudia's "startup menu still present
// after N auto-confirmations" timeout, which is what a trust dialog
// produces when Enter alone does not satisfy it.
func isUndismissedMenu(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "startup menu") &&
		strings.Contains(low, "auto-confirmation")
}

// frameOf returns the pane text after "last frame:", or the whole
// message when there is no such marker — an undismissed-menu error
// carries the frame the same way, and a caller handing us a bare frame
// should still be answered.
func frameOf(msg string) string {
	if _, after, ok := strings.Cut(msg, "last frame:"); ok {
		return strings.TrimSpace(after)
	}
	return msg
}

// WorkspaceTrustFrame returns the trust dialog's pane text, or "" when
// msg is not a workspace-trust stall.
func WorkspaceTrustFrame(msg string) string {
	if !IsWorkspaceTrust(msg) {
		return ""
	}
	return strings.TrimSpace(frameOf(msg))
}

// workspaceTrustCopy is the owner-visible text for the class. It names
// the workdir-shaped fix rather than the generic "retried", because
// retrying is what lost the seats.
func workspaceTrustCopy(raw string) string {
	return "Agent CLI is waiting on Claude Code's workspace-trust dialog (workspace_trust). " +
		"The process is alive and the composer will never draw: this modal wants a human keypress, " +
		"so it does NOT clear by retrying and the seat must not be reaped as a stall. " +
		"Resolve it by trusting that workdir once — run `claude -p ok` in it, or let jevons pre-accept " +
		"trust at mint (internal/claudetrust), then re-mint the seat. Last frame: " +
		truncate(WorkspaceTrustFrame(raw), 400)
}
