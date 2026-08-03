// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T222 hermetic: file twice with same mission fixture → one id (duplicate hit).
func TestFindDuplicateExactAndNearName(t *testing.T) {
	open := []OpenLeaf{
		{
			ID:         "T220",
			Name:       "RHS inspect renders fleet user injects as markdown",
			Acceptance: []string{"user injects and MD-shaped user turns render"},
			Status:     "identified",
		},
		{
			ID:     "T10.2",
			Name:   "Server Peer + owned tables live again",
			Status: "identified",
		},
	}

	// Exact-ish near: PO re-files same gap as overseer.
	dup := FindDuplicate(open, Proposal{
		Name:       "RHS inspect renders fleet user injects as markdown",
		Acceptance: []string{"user injects and MD-shaped user turns render"},
	})
	if dup == nil || dup.ExistingID != "T220" {
		t.Fatalf("exact name: got %+v", dup)
	}
	if dup.Reason != "exact_name" {
		t.Fatalf("reason=%q", dup.Reason)
	}

	// Near name containment / shared tokens (T220 vs T221 style).
	dup2 := FindDuplicate(open, Proposal{
		Name:       "RHS inspect user-MD for fleet injects",
		Acceptance: []string{"fleet user injects render as MD"},
	})
	if dup2 == nil || dup2.ExistingID != "T220" {
		t.Fatalf("near name: got %+v", dup2)
	}

	// Unrelated name must not match.
	if d := FindDuplicate(open, Proposal{Name: "Completely unrelated billing tax form"}); d != nil {
		t.Fatalf("want nil for unrelated, got %+v", d)
	}
}

func TestFindDuplicateMissionKey(t *testing.T) {
	open := []OpenLeaf{
		{ID: "T100", Name: "Alpha", Status: "identified", MissionKey: "inspect-user-md"},
	}
	dup := FindDuplicate(open, Proposal{
		Name:       "Totally different wording",
		MissionKey: "inspect-user-md",
	})
	if dup == nil || dup.ExistingID != "T100" || dup.Reason != "mission_key" {
		t.Fatalf("got %+v", dup)
	}
}

func TestFindDuplicateForceOverride(t *testing.T) {
	open := []OpenLeaf{
		{ID: "T1", Name: "Same name forever", Status: "identified"},
	}
	if d := FindDuplicate(open, Proposal{Name: "Same name forever", Force: true}); d != nil {
		t.Fatalf("force must allow split, got %+v", d)
	}
}

func TestFindDuplicateSkipsClosedLeaves(t *testing.T) {
	open := []OpenLeaf{
		{ID: "T220", Name: "Inspect user MD", Status: "set_aside"},
		{ID: "T221", Name: "Inspect user MD active", Status: "identified"},
	}
	// Near-match should prefer open T221, not set_aside T220.
	// Exact on closed alone does not block.
	if d := FindDuplicate([]OpenLeaf{open[0]}, Proposal{Name: "Inspect user MD"}); d != nil {
		t.Fatalf("closed leaf must not dedupe: %+v", d)
	}
	dup := FindDuplicate(open, Proposal{Name: "Inspect user MD active"})
	if dup == nil || dup.ExistingID != "T221" {
		t.Fatalf("got %+v", dup)
	}
}

func TestExtractMissionKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"mission_key:inspect-user-md from owner", "inspect-user-md"},
		{"same_mission=foo-md-t220", "foo-md-t220"},
		{"no key here", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ExtractMissionKey(tc.in); got != tc.want {
			t.Errorf("ExtractMissionKey(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadOpenLeavesFromYAML(t *testing.T) {
	yaml := `
targets:
  T220:
    name: Inspect user MD
    status: identified
    acceptance:
      - user injects render
    context: "mission_key:inspect-user-md"
  T999:
    name: Done thing
    status: achieved
  T221:
    name: Dup open
    status: set_aside
`
	leaves, err := LoadOpenLeavesFromYAML([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if len(leaves) != 1 || leaves[0].ID != "T220" {
		t.Fatalf("leaves=%+v", leaves)
	}
	if leaves[0].MissionKey != "inspect-user-md" {
		t.Fatalf("mission key=%q", leaves[0].MissionKey)
	}
}

func TestLoadOpenLeavesFromCwd(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`
targets:
  T1:
    name: One leaf
    status: identified
    acceptance: ["a"]
`)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	leaves, err := LoadOpenLeavesFromCwd(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaves) != 1 || leaves[0].ID != "T1" {
		t.Fatalf("got %+v", leaves)
	}
}

func TestGateKickoff(t *testing.T) {
	// Engaged → refuse.
	d := GateKickoff("identified", []string{"jv-t221-inspect-user-md"}, false)
	if d.Allow || d.Reason != "already_engaged" {
		t.Fatalf("engaged: %+v", d)
	}
	if !strings.Contains(d.Message, "jv-t221-inspect-user-md") {
		t.Fatalf("message=%q", d.Message)
	}

	// set_aside / achieved.
	if d := GateKickoff("set_aside", nil, false); d.Allow || d.Reason != "set_aside" {
		t.Fatalf("set_aside: %+v", d)
	}
	if d := GateKickoff("achieved", nil, false); d.Allow || d.Reason != "achieved" {
		t.Fatalf("achieved: %+v", d)
	}

	// Free open target.
	if d := GateKickoff("identified", nil, false); !d.Allow {
		t.Fatalf("free: %+v", d)
	}

	// Force overrides engagement.
	if d := GateKickoff("identified", []string{"w"}, true); !d.Allow {
		t.Fatalf("force: %+v", d)
	}
}

func TestAttachMessage(t *testing.T) {
	msg := AttachMessage(&Duplicate{ExistingID: "T220", Reason: "near_name"}, "Inspect")
	if !strings.Contains(msg, "🎯T220") || !strings.Contains(msg, "__TARGET_FILED__:T220") {
		t.Fatalf("msg=%q", msg)
	}
	if !strings.Contains(msg, "no new id allocated") {
		t.Fatalf("msg=%q", msg)
	}
}

func TestLookupTargetStatus(t *testing.T) {
	yaml := []byte(`
targets:
  T220:
    name: X
    status: set_aside
`)
	st, ok := LookupTargetStatus(yaml, "🎯T220")
	if !ok || st != "set_aside" {
		t.Fatalf("st=%q ok=%v", st, ok)
	}
}
