// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

// 🎯T326: agent-facing target ids always carry the 🎯 prefix.
func TestFormatTargetID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"T326", "🎯T326"},
		{"🎯T326", "🎯T326"},
		{" 🎯T27.2 ", "🎯T27.2"},
		{"t10", "🎯T10"},
		{"T31.1", "🎯T31.1"},
	}
	for _, c := range cases {
		got := FormatTargetID(c.in)
		if got != c.want {
			t.Errorf("FormatTargetID(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestFleetStandingBriefHasNoBareTargetIDs(t *testing.T) {
	if !HasBareTargetID("see T305 next") {
		t.Fatal("detector should flag bare T305")
	}
	if HasBareTargetID("see 🎯T305 next") {
		t.Fatal("detector must not flag 🎯T305")
	}
	// Worker names use lowercase t — not bare target ids.
	if HasBareTargetID("jv-t27.2-config") {
		t.Fatal("lowercase worker names must not count as bare T-ids")
	}
	if HasBareTargetID(FleetStandingBrief) {
		t.Fatalf("FleetStandingBrief still has bare T-ids (🎯T326):\n%s", FleetStandingBrief)
	}
	// Inject path samples must contain the emoji prefix for core doctrine ids.
	for _, want := range []string{
		"🎯T176", "🎯T104", "🎯T31", "🎯T31.1", "🎯T31.2",
		"🎯T78", "🎯T197", "🎯T111.4", "🎯T155", "🎯T193", "🎯T262.1",
		"🎯T325.1", "🎯T125", "🎯T129", "🎯T130", "🎯T194",
		"🎯T112", "🎯T67", "🎯T29", "🎯T244",
	} {
		if !strings.Contains(FleetStandingBrief, want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

func TestIdleNudgeTextUsesEmojiPrefix(t *testing.T) {
	got := FormatIdleNudgeText(IdleNudgeTextArgs{
		Name: "jv-t326", TargetID: "T326", Kind: IdleNudgeKindContinue,
	})
	if HasBareTargetID(got) {
		t.Fatalf("continue nudge has bare T-id:\n%s", got)
	}
	if !strings.Contains(got, "🎯T326") {
		t.Fatalf("missing 🎯T326 in:\n%s", got)
	}
	if !strings.Contains(got, "🎯T104") {
		t.Fatalf("missing 🎯T104 in:\n%s", got)
	}
	full := FormatIdleNudgeText(IdleNudgeTextArgs{
		Name: "jv-t326", TargetID: "T326", Kind: IdleNudgeKindFullBrief,
	})
	if HasBareTargetID(full) {
		// Standing brief is included — must also be clean.
		t.Fatalf("full_brief nudge has bare T-id:\n%s", full)
	}
	if !strings.Contains(full, "Mission target: 🎯T326") {
		t.Fatalf("mission line missing emoji form:\n%s", full)
	}
}
