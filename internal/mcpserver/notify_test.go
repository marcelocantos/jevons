// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

// 🎯T46: the CEO prompt tells the overseer that worker replies arrive as
// notifications pushed into its conversation — this pins the mechanism
// behind that promise. If notify stops delivering (or stops naming the
// agent), the prompt's contract is broken and this fails.

func TestNotifyDeliversAgentReplyToOverseer(t *testing.T) {
	s := &Server{}

	var got string
	s.SetNotify(func(text string) { got = text })
	s.notify("maze-worker", "done: PR #7 is green")

	if !strings.Contains(got, "maze-worker") {
		t.Fatalf("notification does not name the agent: %q", got)
	}
	if !strings.Contains(got, "done: PR #7 is green") {
		t.Fatalf("notification lost the reply text: %q", got)
	}
}

// A worker reply with no notify sink must not panic — the daemon may not
// have attached the overseer yet.
func TestNotifyWithoutSinkIsSafe(t *testing.T) {
	s := &Server{}
	s.notify("orphan", "hello")
}

// Overlong replies are truncated, not dropped.
func TestNotifyTruncatesLongReplies(t *testing.T) {
	s := &Server{}
	var got string
	s.SetNotify(func(text string) { got = text })

	s.notify("w", strings.Repeat("x", 5000))
	if len(got) > 2100 {
		t.Fatalf("notification not truncated: len = %d", len(got))
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("truncated notification lacks ellipsis marker: %q", got)
	}
}
