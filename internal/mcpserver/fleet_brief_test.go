// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

// 🎯T104 under fan-out: first agent_send injects local-delivery doctrine
// on the shipped path (not only persona greps).
func TestEnsureFleetBriefInjectsOnce(t *testing.T) {
	m := map[string]bool{}
	out, inj := EnsureFleetBrief(m, "worker", "implement the fix")
	if !inj {
		t.Fatal("expected inject on first send")
	}
	for _, want := range []string{
		"Jevons fleet standing brief",
		"local by default",
		"Do NOT open GitHub PRs",
		"local commits",
		"spawn_subagent",
		"Multi-slice fan-out",
		"T111.4",
		"Unattended frontier auto-spawn",
		"T155",
		"parent=jevons-po",
		"needs-owner",
		"design-discussion",
		"parked-for-design",
		"T112",
		"T67",
		"T29-class",
		"PO never implements",
		"T125",
		"spawn-only for Build work",
		"instructional doctrine",
		"Overseer never parents product workers",
		"T129",
		"parent=jevons",
		"jevons-po",
		"Filing reflex",
		"T130",
		"standing rule",
		"going forward",
		"from now on",
		"we should always",
		"jevons_target_file",
		"bullseye_commit",
		"T92",
		"implement the fix",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	out2, inj2 := EnsureFleetBrief(m, "worker", "follow-up")
	if inj2 {
		t.Fatal("second send must not re-inject")
	}
	if out2 != "follow-up" {
		t.Fatalf("got %q", out2)
	}
}

func TestEnsureFleetBriefIdempotentWhenCallerIncluded(t *testing.T) {
	m := map[string]bool{}
	body := FleetStandingBrief + "already briefed task"
	out, inj := EnsureFleetBrief(m, "w", body)
	if inj {
		t.Fatal("should not double-wrap")
	}
	if out != body {
		t.Fatal("text mutated")
	}
	if !m["w"] {
		t.Fatal("should mark briefed")
	}
}
