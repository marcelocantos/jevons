// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// 🎯T743: when mintProviderPick drops an explicit ineligible pin, the start
// result (route note / Cite) names asked/minted/because — not only a
// provider_knob field. A mutation that clears AskedProvider silently goes red.
func TestT743IneligibleExplicitPinNamesSubstitutionOnResult(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t652HotWeekly("grok", now),
			t583Weekly("claude", 60, now),
		}
	}, now)

	pick := s.mintProviderPick("grok", "", false, "code_implement", string(claudia.PurposeWork), "jv-t743-result", false)
	if pick.Provider == cost.HarnessGrok {
		t.Fatalf("hot grok pin stuck: %+v", pick)
	}
	if pick.AskedProvider != "grok" {
		t.Fatalf("AskedProvider not recorded (silent drop): %+v", pick)
	}
	if pick.DropReason != "ineligible" {
		t.Fatalf("DropReason = %q, want ineligible", pick.DropReason)
	}
	cite := pick.Cite()
	wantSub := fmt.Sprintf("substituted: asked=grok minted=%s because grok was ineligible", pick.Provider)
	if !strings.Contains(cite, wantSub) {
		t.Fatalf("cite %q missing substitution %q — silent drop would pass", cite, wantSub)
	}

	def, _, note, err := s.stitchAgentStart(
		"jv-t743-result", t.TempDir(), "", "grok", "",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if def.Provider == claudia.ProviderGrok {
		t.Fatalf("stitch kept grok: provider=%q note=%q", def.Provider, note)
	}
	if !strings.Contains(note, "substituted: asked=grok") {
		t.Fatalf("stitch route note missing asked=grok: %q", note)
	}
	if !strings.Contains(note, "minted=") {
		t.Fatalf("stitch route note missing minted=: %q", note)
	}
	if !strings.Contains(note, "because grok was ineligible") {
		t.Fatalf("stitch route note missing because: %q", note)
	}

	msg := formatAgentStartResult(
		"jv-t743-result", "/tmp/w", "jevons-po", "work", "worker", "",
		string(def.Provider), "", "sess", note, "",
	)
	if !strings.Contains(msg, "substituted: asked=grok") {
		t.Fatalf("start result missing substitution: %q", msg)
	}

	// Mutation pin: a Cite that drops AskedProvider must not satisfy the
	// acceptance. Prove the oracle would go red.
	silent := pick
	silent.AskedProvider = ""
	silent.DropReason = ""
	if strings.Contains(silent.Cite(), "substituted:") {
		t.Fatal("mutation control failed: empty AskedProvider still cites substitution")
	}
}

// 🎯T743: a launch/register failure after substitution names asked/minted
// in the error the caller (and parent via notifySpawnFailure) receives.
func TestT743LaunchFailureNamesSubstitution(t *testing.T) {
	route := "provider_knob: claudia, task_type: code_implement, substituted: asked=grok minted=cursor because grok was ineligible (compiled_seed would have picked grok via code_implement)"
	got := formatStartFailure(fmt.Errorf("timeout waiting for turn begin"), route)
	for _, want := range []string{
		"start failed: timeout waiting for turn begin",
		"substituted: asked=grok minted=cursor because grok was ineligible",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("failure text %q missing %q", got, want)
		}
	}
	// No trailing loser-knob parenthetical must leak into the bracket.
	if strings.Contains(got, "compiled_seed") {
		t.Fatalf("failure text kept loser-knob tail: %q", got)
	}

	// Silent route note (pre-T743) must not invent a substitution.
	if sub := substitutionFromRouteNote("provider_knob: claudia"); sub != "" {
		t.Fatalf("no-sub route note produced %q", sub)
	}
	plain := formatStartFailure(fmt.Errorf("launch hung"), "provider_knob: claudia")
	if strings.Contains(plain, "substituted:") {
		t.Fatalf("plain failure invented substitution: %q", plain)
	}
}

// 🎯T743: eligible explicit, existed=true resume, and owner_asked keep
// winning unchanged (T148 / T583 tape 3 / T652).
func TestT743EligibleAndOwnerAskedUnchanged(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	s := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t583Weekly("grok", 60, now),
			t583Weekly("claude", 50, now),
		}
	}, now)

	// Eligible explicit sticks, no substitution.
	pick := s.mintProviderPick("grok", "", false, "", string(claudia.PurposeWork), "jv-t743-eligible", false)
	if pick.Provider != cost.HarnessGrok || pick.Knob != cost.KnobExplicit {
		t.Fatalf("eligible explicit = %+v", pick)
	}
	if pick.AskedProvider != "" || strings.Contains(pick.Cite(), "substituted:") {
		t.Fatalf("eligible explicit recorded a drop: %+v cite=%q", pick, pick.Cite())
	}

	// owner_asked keeps a hot pin (no drop, no substitution).
	sHot := t583Server(t, func(now time.Time) []planusage.Backend {
		return []planusage.Backend{
			t652HotWeekly("grok", now),
			t583Weekly("claude", 60, now),
		}
	}, now)
	owned := sHot.mintProviderPick("grok", "", false, "", string(claudia.PurposeWork), "jv-t743-owned", true)
	if owned.Provider != cost.HarnessGrok || owned.Knob != cost.KnobExplicit {
		t.Fatalf("owner_asked = %+v", owned)
	}
	if owned.AskedProvider != "" {
		t.Fatalf("owner_asked recorded a drop: %+v", owned)
	}

	// Resume with stored provider: no drop.
	resume := s.mintProviderPick("", "claude", true, "", string(claudia.PurposeWork), "jv-t743-resume", false)
	if resume.Provider != cost.HarnessClaude || resume.Knob != cost.KnobResume {
		t.Fatalf("resume = %+v", resume)
	}
	if resume.AskedProvider != "" {
		t.Fatalf("resume recorded a drop: %+v", resume)
	}
}
