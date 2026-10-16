// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/roles"
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
		"Commit when done",
		"🎯T638",
		"only commit when asked",
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
		// 🎯T511 role doctrine lives in role files
		"Role doctrine is per-role",
		"🎯T511",
		"if-you-are-X",
		"internal/roles/builtin",
		// 🎯T262.1 frontier = ready set
		"Frontier = ready set",
		"🎯T262.1",
		"next ticket",
		"unblocked ready leaves",
		"indifferent or policy",
		"engagement",
		"Anti-pattern",
		"frontier-as-ready-set.md",
		"Filing reflex",
		"🎯T130",
		"standing rule",
		"going forward",
		"from now on",
		"we should always",
		"jevons_target_file",
		"bullseye_commit",
		"🎯T546",
		"StrReplace",
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
		"development or released",
		"🎯T572",
		// 🎯T692 fleet state narration
		"Fleet state narration matches the live registry",
		"🎯T692",
		"killing",
		"will stop",
		"killed",
		"is stopped",
		"jv-t679.2-born-stuck",
		"claiming a running seat is dead",
		"LooksLikeUnverifiedLifecycleClaim",
		"ClassifyLifecycleNarration",
		// 🎯T552 / T553.2 owner-visible observation (was T194)
		"Owner-visible claims are observed",
		"🎯T552",
		"🎯T553.2",
		"🎯T194",
		"necessary not sufficient",
		"restart-jevonsd.sh",
		"live probe",
		"HasActivationEvidence",
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
		"Parent report is daemon-delivered",
		"🎯T690",
		"parent_report: daemon-delivered",
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

// 🎯T493.1: standing brief carries the visual-cockpit prose-look doctrine.
func TestFleetStandingBriefVisualCockpitProseVerdict(t *testing.T) {
	for _, want := range []string{
		"Visual cockpit finish is a prose look, not a green metric",
		"🎯T493.1",
		"#messages",
		"normal chat transcript after a hard reload",
		"visibleInScroller",
		"screenshot-tool caption",
		"automatic no",
		"HasVisualProseVerdict",
		"LooksLikeMissingVisualVerdict",
	} {
		if !strings.Contains(FleetStandingBrief, want) {
			t.Errorf("FleetStandingBrief missing T493.1 marker %q", want)
		}
	}
}

// 🎯T692: standing brief carries live-registry narration doctrine.
func TestFleetStandingBriefLifecycleNarration(t *testing.T) {
	for _, want := range []string{
		"Fleet state narration matches the live registry",
		"🎯T692",
		"killing",
		"will stop",
		"killed",
		"is stopped",
		"jv-t679.2-born-stuck",
		"claiming a running seat is dead",
		"LooksLikeUnverifiedLifecycleClaim",
		"ClassifyLifecycleNarration",
		"jevons_agent_list",
		"GET /api/agents",
	} {
		if !strings.Contains(FleetStandingBrief, want) {
			t.Errorf("FleetStandingBrief missing T692 marker %q", want)
		}
	}
	if LooksLikeUnverifiedLifecycleClaim(FleetStandingBrief) {
		t.Fatal("injected standing brief must not self-flag as unverified lifecycle narration")
	}
}

// 🎯T536.3: fog-of-war scout doctrine lives on the worker role (🎯T511).
func TestFleetStandingBriefFogOfWarScout(t *testing.T) {
	def, err := rolesCatalogBuiltin("worker")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Fog-of-war scout before implement",
		"🎯T536.3",
		"phase scout",
		"scout-report",
		"fog-known",
		"InheritLedger",
	} {
		if !strings.Contains(def.Body, want) {
			t.Errorf("worker role missing T536.3 marker %q", want)
		}
	}
	if strings.Contains(FleetStandingBrief, "Fog-of-war scout before implement") {
		t.Fatal("standing brief must not still carry fog-of-war role doctrine (🎯T511)")
	}
}

// 🎯T693: standing brief ranks dest on published band; Fable spent is not Claude down.
func TestFleetStandingBriefPlanDestBandFirst(t *testing.T) {
	for _, want := range []string{
		"Plan dest ranks published band first",
		"🎯T693",
		"**under** outranks **ok**",
		"hot** and **ahead** are never destinations",
		"Fable spent ≠ Claude unavailable",
	} {
		if !strings.Contains(FleetStandingBrief, want) {
			t.Errorf("FleetStandingBrief missing T693 marker %q", want)
		}
	}
}

// 🎯T690: standing brief names the daemon parent-report channel.
func TestFleetStandingBriefParentReportDaemonDelivered(t *testing.T) {
	for _, want := range []string{
		"Parent report is daemon-delivered",
		"🎯T690",
		"parent_report: daemon-delivered",
		"per-seat tool-approval policy",
		"jevons_agent_send",
	} {
		if !strings.Contains(FleetStandingBrief, want) {
			t.Errorf("FleetStandingBrief missing T690 marker %q", want)
		}
	}
}

// 🎯T1053: quiet-report rule lives on the overseer role (🎯T511).
func TestFleetBriefRoutineReportQuiet(t *testing.T) {
	def, err := rolesCatalogBuiltin("overseer")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Routine worker reports are not owner-facing appraisals", "🎯T1053",
		"[silent]", "genuine owner decision point", "anomaly/problem",
		"direct question", "status explicitly requested",
	} {
		if !strings.Contains(def.Body, want) {
			t.Errorf("overseer role missing T1053 marker %q", want)
		}
	}
	if strings.Contains(FleetStandingBrief, "Routine worker reports are not owner-facing appraisals") {
		t.Fatal("standing brief must not still carry overseer T1053 doctrine (🎯T511)")
	}
}

func rolesCatalogBuiltin(name string) (roles.Def, error) {
	return (roles.Catalog{}).Resolve(name)
}

// 🎯T511: standing brief has no if-you-are-X role conditionals; role doctrine
// lives in builtin role files and is assembled on inject-with-role.
func TestT511StandingBriefHasNoRoleConditionals(t *testing.T) {
	for _, bad := range []string{
		"If you are a Stratum-1 product owner",
		"If you are a product owner",
		"if you are an auditor",
		"PO never implements",
		"Overseer never parents product workers",
		"Unattended frontier auto-spawn",
		"File→spawn same turn",
		"PO proactive-until-empty-then-sleep",
	} {
		if strings.Contains(FleetStandingBrief, bad) {
			t.Errorf("FleetStandingBrief still has role-conditional %q (🎯T511)", bad)
		}
	}
}

func TestT511RoleDoctrineAssembledOnInject(t *testing.T) {
	po, err := rolesCatalogBuiltin("product-owner")
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	out, inj := EnsureFleetBriefWithRole(m, "jevons-po", "file T1 and spawn", po.Body)
	if !inj {
		t.Fatal("expected inject")
	}
	for _, want := range []string{
		"Jevons fleet standing brief",
		roles.DoctrineMarker,
		"PO never implements",
		"🎯T125",
		"spawn-only for Build work",
		"🎯T129",
		"Unattended frontier auto-spawn",
		"🎯T155",
		"File→spawn same turn",
		"🎯T193",
		"PO proactive-until-empty-then-sleep",
		"🎯T325.1",
		"file T1 and spawn",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PO assembled brief missing %q", want)
		}
	}
	// Order: universal < role < mission
	ui := strings.Index(out, "Jevons fleet standing brief")
	ri := strings.Index(out, roles.DoctrineMarker)
	mi := strings.Index(out, "file T1 and spawn")
	if !(ui < ri && ri < mi) {
		t.Fatalf("assemble order want universal < role < mission, got %d %d %d", ui, ri, mi)
	}
}

func TestT511WorkerRoleCarriesTerminalDoctrine(t *testing.T) {
	w, err := rolesCatalogBuiltin("worker")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"🎯T31", "🎯T509", "🎯T165", "🎯T195", "silent-ledger", "finish-report",
	} {
		if !strings.Contains(w.Body, want) {
			t.Errorf("worker role missing %q", want)
		}
	}
}

func TestT511OverseerRoleComposesWithPersonaNote(t *testing.T) {
	o, err := rolesCatalogBuiltin("overseer")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"persona.md", "🎯T44", "🎯T129", "🎯T1053", "composes",
	} {
		if !strings.Contains(o.Body, want) {
			t.Errorf("overseer role missing %q", want)
		}
	}
}
