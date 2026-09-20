// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "testing"

// 🎯T692: a past-tense kill/stop/reap/respawn claim without a live-list
// result cited earlier in the same turn is unverified narration. The
// classifier is text-only — a live registry probe is not a substitute
// (registry-only truth is insufficient without the narration gate).

const t692Specimen = "I killed jv-t679.2-born-stuck during PO cleanup."

func TestClassifyLifecycleNarration(t *testing.T) {
	const verified = "" +
		"jevons_agent_list: jv-t679.2-born-stuck is not among running seats " +
		"(finished-and-reaped). Killed jv-t679.2-born-stuck."
	const verifiedAPI = "" +
		"GET /api/agents returned no row for jv-t679.2-born-stuck. " +
		"jv-t679.2-born-stuck is stopped."
	const intentKill = "killing jv-t679.2-born-stuck next"
	const intentWill = "will stop jv-t679.2-born-stuck after this turn"
	const citationAfter = "" +
		"I killed jv-t679.2-born-stuck. " +
		"jevons_agent_list: not running."
	const toolNameOnly = "" +
		"The required check is jevons_agent_list. I killed jv-t679.2-born-stuck."
	cases := []struct {
		name string
		in   string
		want LifecycleNarrationClass
	}{
		{"empty", "", LifecycleNarrationNone},
		{"no lifecycle", "in progress: reading the born-stuck path", LifecycleNarrationNone},
		{"doctrine vocabulary, no seat", "completed fact ('killed', 'is stopped') needs a citation", LifecycleNarrationNone},
		{"specimen kill as fact", t692Specimen, LifecycleNarrationUnverified},
		{"is stopped as fact", "jv-t679.2-born-stuck is stopped.", LifecycleNarrationUnverified},
		{"reaped as fact", "Reaped jv-t679.2-born-stuck.", LifecycleNarrationUnverified},
		{"respawned as fact", "Respawned jevons-po onto a fresh session.", LifecycleNarrationUnverified},
		{"citation after the claim", citationAfter, LifecycleNarrationUnverified},
		{"tool name without a result", toolNameOnly, LifecycleNarrationUnverified},
		{"live list result then kill", verified, LifecycleNarrationVerified},
		{"GET /api/agents result then is stopped", verifiedAPI, LifecycleNarrationVerified},
		{"intent killing", intentKill, LifecycleNarrationIntent},
		{"intent will stop", intentWill, LifecycleNarrationIntent},
		{"about to reap", "about to reap jv-t679.2-born-stuck", LifecycleNarrationIntent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyLifecycleNarration(tc.in)
			if got != tc.want {
				t.Fatalf("ClassifyLifecycleNarration(%q)=%s want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestLooksLikeUnverifiedLifecycleClaim(t *testing.T) {
	if !LooksLikeUnverifiedLifecycleClaim(t692Specimen) {
		t.Fatal("specimen (overseer claimed a kill while the seat was running) must flag")
	}
	if LooksLikeUnverifiedLifecycleClaim("killing jv-t679.2-born-stuck") {
		t.Fatal("intent/plan must not flag")
	}
	good := "jevons_agent_list: jv-t679.2-born-stuck omitted from running. Killed jv-t679.2-born-stuck."
	if LooksLikeUnverifiedLifecycleClaim(good) {
		t.Fatal("live-list result cited before the fact must not flag")
	}
}

func TestLifecycleNarrationDoesNotConsultRegistry(t *testing.T) {
	// Acceptance: registry-only truth is insufficient without the narration
	// gate. This function has no registry parameter; a running seat in the
	// live list cannot make an unverified sentence pass.
	if ClassifyLifecycleNarration(t692Specimen) != LifecycleNarrationUnverified {
		t.Fatal("text-only specimen must stay unverified regardless of any live list")
	}
}

func TestFleetStandingBriefDoesNotSelfFlagLifecycleNarration(t *testing.T) {
	got := ClassifyLifecycleNarration(FleetStandingBrief)
	if got == LifecycleNarrationUnverified {
		t.Fatalf("standing brief must not self-flag as unverified lifecycle narration; got %s", got)
	}
}

func TestLifecycleNarrationClassString(t *testing.T) {
	if LifecycleNarrationUnverified.String() != "unverified" {
		t.Fatalf("got %q", LifecycleNarrationUnverified.String())
	}
	if LifecycleNarrationVerified.String() != "verified" {
		t.Fatalf("got %q", LifecycleNarrationVerified.String())
	}
	if LifecycleNarrationIntent.String() != "intent" {
		t.Fatalf("got %q", LifecycleNarrationIntent.String())
	}
	if LifecycleNarrationNone.String() != "none" {
		t.Fatalf("got %q", LifecycleNarrationNone.String())
	}
}
