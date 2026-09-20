// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// The launch error under ge-po's repeated disappearance on 2026-09-20
// (🎯T709). claudia names the reason no_composer — truthfully, the
// composer never drew — and the frame says why.
const t709TrustErr = "send failed: claude not ready (no_composer): no idle input box after 30s; last frame:\n" +
	"╭──────────────────────────────────────────────╮\n" +
	"│ Quick safety check: Is this a project you    │\n" +
	"│ created or one you trust?                    │\n" +
	"│                                              │\n" +
	"│ ❯ 1. Yes, I trust this folder                │\n" +
	"│   2. No, exit                                │\n" +
	"╰──────────────────────────────────────────────╯"

// The older generation of the same dialog, reached through claudia's
// undismissed-menu timeout rather than a ready-pattern one.
const t709TrustMenuErr = "startup menu (e.g. Claude Code's resume/summary prompt) still present after 3 auto-confirmations within 45s; last frame:\n" +
	"Do you trust the files in this folder?\n" +
	"❯ 1. Yes, proceed\n" +
	"  2. No, exit"

// 🎯T709 clause 1: the trust modal is its own class, not a stall.
//
// Before the fix this frame classified as startup_stall, whose owner
// copy ends "the seat is retried" — and retrying is precisely what
// killed the seat. A modal waiting on a keypress does not clear because
// a timeout fired again.
func TestT709WorkspaceTrustClass(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"no_composer/ready-timeout": t709TrustErr,
		"undismissed startup menu":  t709TrustMenuErr,
	} {
		if got := agenterr.ClassifyText(in); got != agenterr.ClassWorkspaceTrust {
			t.Errorf("%s: class=%q want workspace_trust", name, got)
		}
		if got := agenterr.WorkspaceTrustFrame(in); !strings.Contains(strings.ToLower(got), "trust") {
			t.Errorf("%s: frame must carry the dialog, got %q", name, got)
		}
	}
}

// 🎯T709 clause 2: not transient. "Wait and retry" is the answer that
// lost the seats, so the class must not be re-pressure-worthy, and it
// must still read as a failure rather than falling through to none.
func TestT709WorkspaceTrustIsNotRetried(t *testing.T) {
	t.Parallel()
	if agenterr.ClassWorkspaceTrust.IsTransient() {
		t.Fatal("a modal awaiting a keypress must not be classed as retriable")
	}
	if !agenterr.ClassWorkspaceTrust.IsFailure() {
		t.Fatal("workspace_trust must be a failure, not none")
	}
	fields := agenterr.FailureFields(agenterr.ClassWorkspaceTrust, t709TrustErr, nil)
	if fields["failure_class"] != "workspace_trust" || fields["transient"] != false {
		t.Fatalf("slog fields must name the class and say not-transient: %v", fields)
	}
}

// 🎯T709 clause 3: the owner copy is a recoverable action, quoting the
// frame — not the silent reap and not "retried".
func TestT709WorkspaceTrustOwnerCopyIsActionable(t *testing.T) {
	t.Parallel()
	class, msg := agenterr.ClassifyAndFormat(errors.New(t709TrustErr))
	if class != agenterr.ClassWorkspaceTrust {
		t.Fatalf("class=%q", class)
	}
	for _, want := range []string{"workspace_trust", "claude -p", "Quick safety check"} {
		if !strings.Contains(msg, want) {
			t.Errorf("owner copy missing %q: %q", want, msg)
		}
	}
	if strings.Contains(msg, "the seat is retried") {
		t.Errorf("owner copy must not promise a retry that cannot work: %q", msg)
	}
}

// 🎯T709 negatives. The marker is a full phrase because this decides
// whether the owner is sent to press a key: an agent discussing trust in
// its own pane, and a frame with a live composer, are not modals.
func TestT709WorkspaceTrustNegatives(t *testing.T) {
	t.Parallel()
	cases := map[string]agenterr.Class{
		// Transcript prose about trust, no ready timeout at all.
		"⏺ The supervisor cannot trust the files it did not write": agenterr.ClassNone,
		// A stall whose frame is settings warnings stays startup_stall.
		"claude not ready (settings_warning): last frame:\nPermission deny rule \"TeamCreate\" matches no known tool": agenterr.ClassStartupStall,
		// Composer drawn: neither a trust modal nor a stall.
		"ready pattern did not match within 30s; last frame:\n❯ Try \"fix lint\"": agenterr.ClassNone,
	}
	for in, want := range cases {
		if got := agenterr.ClassifyText(in); got != want {
			t.Errorf("ClassifyText(%q)=%q want %q", in, got, want)
		}
	}
	if agenterr.WorkspaceTrustFrame("connection refused") != "" {
		t.Fatal("WorkspaceTrustFrame on an unrelated error must be empty")
	}
}
