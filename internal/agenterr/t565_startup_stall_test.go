// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// The frame logged on 2026-08-10 / 2026-08-15 / 2026-08-29 (🎯T565 addendum).
const t565StallErr = "send failed: claude not ready: ready pattern did not match within 30s; last frame:\n" +
	"Permission deny rule \"TeamCreate\" matches no known tool — check for typos.\n" +
	"Permission deny rule \"TeamDelete\" matches no known tool — check for typos.\n\n\n\n\n\n\n"

// 🎯T565: settings warnings over an undrawn TUI are a startup stall — not
// none (the seat was retired on that), not auth (the frame says "Permission").
func TestT565StartupStallClass(t *testing.T) {
	t.Parallel()
	if got := agenterr.Classify(errors.New(t565StallErr)); got != agenterr.ClassStartupStall {
		t.Fatalf("class=%q want startup_stall", got)
	}
	if !agenterr.ClassStartupStall.IsTransient() {
		t.Fatal("a stall is retried, not failed closed")
	}
	// The owner copy names the last frame verbatim.
	_, msg := agenterr.ClassifyAndFormat(errors.New(t565StallErr))
	if !strings.Contains(msg, `Permission deny rule "TeamCreate" matches no known tool`) || !strings.Contains(msg, "startup_stall") {
		t.Fatalf("owner copy must quote the frame: %q", msg)
	}
	if got := agenterr.LastFrame(t565StallErr); !strings.HasPrefix(got, "Permission deny rule") || strings.HasSuffix(got, "\n") {
		t.Fatalf("LastFrame=%q", got)
	}
}

func TestT565StartupStallNegatives(t *testing.T) {
	t.Parallel()
	cases := map[string]agenterr.Class{
		// Composer drawn: a pattern mismatch, not a stall.
		"ready pattern did not match within 30s; last frame:\n──────────\n❯ Try \"fix lint\"\n──────────": agenterr.ClassNone,
		// Transcript on screen, no warnings: not startup output.
		"ready pattern did not match within 30s; last frame:\n⏺ Reading files…\n": agenterr.ClassNone,
		// Same warning text without a ready timeout is not a stall.
		"Permission deny rule \"TeamCreate\" matches no known tool": agenterr.ClassNone,
	}
	for in, want := range cases {
		if got := agenterr.ClassifyText(in); got != want {
			t.Errorf("ClassifyText(%q)=%q want %q", in, got, want)
		}
	}
	if agenterr.LastFrame("connection refused") != "" {
		t.Fatal("LastFrame on a non-timeout must be empty")
	}
}

func TestT565NamedReadyReasonsAreStartupStall(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		token   string
		wantMsg string
	}{
		{
			in:      "send failed: claude not ready (rc_connecting): /rc connecting still showing after 30s; last frame:\n❯ \n  /rc connecting…",
			token:   "rc_connecting",
			wantMsg: "rc_connecting",
		},
		{
			in:      "claude not ready (settings_warning): Claude Code printed startup settings warnings and never drew its input box within 30s; last frame:\nPermission deny rule \"TeamCreate\" matches no known tool",
			token:   "settings_warning",
			wantMsg: "startup_stall",
		},
		{
			in:      "claude not ready (splash): composer ghost placeholder still drawn after 30s; last frame:\n❯ Try \"fix lint\"",
			token:   "splash",
			wantMsg: "splash",
		},
		{
			in:      "claude not ready (no_composer): no idle input box after 30s; last frame:\n● Rebuilding…",
			token:   "no_composer",
			wantMsg: "no_composer",
		},
		{
			in:      "send failed: Agent CLI stalled on startup (startup_stall / no_composer): no idle input box within the ready timeout — not a cloud outage; the seat is retried. Last frame: Quick safety check: Is this a project you created or one you trust? (Like your\n own code, a well-known open source project, or work from your team).",
			token:   "workspace_trust",
			wantMsg: "workspace_trust",
		},
	}
	for _, c := range cases {
		if got := agenterr.ClassifyText(c.in); got != agenterr.ClassStartupStall {
			t.Errorf("ClassifyText(%q)=%q want startup_stall", c.in, got)
		}
		_, msg := agenterr.ClassifyAndFormat(errors.New(c.in))
		if !strings.Contains(msg, c.wantMsg) {
			t.Errorf("owner copy missing %q: %q", c.wantMsg, msg)
		}
		if got := agenterr.LastFrame(c.in); got == "" {
			t.Errorf("LastFrame empty for named reason %s", c.token)
		}
	}
}
