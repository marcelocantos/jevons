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
		"Oracle-first completion",
		// 🎯T326: inject path always uses emoji prefix (not bare T31).
		"🎯T31",
		"🎯T31.1",
		"Bare \"done\"",
		"accepted-risk",
		"class-3",
		"Attestation ≠ execution",
		"independent gate",
		// 🎯T31.2 greenfield elicitation
		"Greenfield oracle elicitation",
		"🎯T31.2",
		"oracle-coverage",
		"pinned",
		"fuzzy",
		"when X expect Y",
		"SPIRAL",
		"DECIDABLE-FROM-TASTE",
		"CoverageMap",
		"spawn_subagent",
		"Multi-slice fan-out",
		"🎯T111.4",
		"Unattended frontier auto-spawn",
		"🎯T155",
		"parent=jevons-po",
		"needs-owner",
		"design-discussion",
		"parked-for-design",
		"🎯T112",
		"🎯T67",
		"🎯T29-class",
		// 🎯T193 file→spawn same turn
		"File→spawn same turn",
		"🎯T193",
		"Build-plane",
		"same turn",
		"named worker",
		"ledger-only",
		"target:",
		"design-gated",
		"blocked-on-human",
		"docs-only",
		// 🎯T325.1 PO proactive-until-empty-then-sleep
		"PO proactive-until-empty-then-sleep",
		"🎯T325.1",
		"until empty or blocked",
		"one-shot pass",
		"sleep/idle",
		"open-mission",
		"interruptible",
		"ClassifyPOProactive",
		"ClassifyFrontierLeaf",
		"POOpenMissionForProactive",
		"PO never implements",
		"🎯T125",
		"spawn-only for Build work",
		"instructional doctrine",
		"Overseer never parents product workers",
		"🎯T129",
		"parent=jevons",
		"jevons-po",
		"Filing reflex",
		"🎯T130",
		"standing rule",
		"going forward",
		"from now on",
		"we should always",
		"jevons_target_file",
		"bullseye_commit",
		"🎯T92",
		// 🎯T176 status language
		"Status language: in progress vs live",
		"🎯T176",
		"in progress",
		"not yet owner-visible",
		"Never call a registered/running worker",
		"landed",
		"shipped",
		"hard-reloadable UI",
		"proven API",
		"daily path",
		// 🎯T194 daily-path achieve evidence
		"Achieve reports need activated daily path",
		"🎯T194",
		"necessary not sufficient",
		"restart-daily-jevonsd.sh",
		"live probe",
		"HasDailyPathEvidence",
		"hermetics alone",
		"stale binary",
		// 🎯T197 worker names: literal dots, never digit-squash
		"Worker names: literal dots for hierarchical ids",
		"🎯T197",
		"jv-t27.2-config",
		"jv-t272-config",
		"digit-squash",
		"jv-t159-seal",
		"literal dots",
		"implement the fix",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if HasBareTargetID(out) {
		t.Error("fleet brief inject still contains bare T-ids (🎯T326)")
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
