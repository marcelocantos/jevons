// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"strings"
	"testing"
)

// 🎯T743: a dropped explicit pin is named on Cite as asked/minted/because,
// and a mutation that omits the substitution goes red.
func TestT743SubstitutionCiteNamesAskedMinted(t *testing.T) {
	p := MintProviderPick{
		Provider:      "cursor",
		Knob:          KnobClaudia,
		TaskType:      TaskCodeImplement,
		AskedProvider: "grok",
		DropReason:    "ineligible",
	}
	cite := p.Cite()
	for _, want := range []string{
		"substituted: asked=grok minted=cursor because grok was ineligible",
		"provider_knob: claudia",
	} {
		if !strings.Contains(cite, want) {
			t.Fatalf("cite %q missing %q", cite, want)
		}
	}
	sub := p.SubstitutionCite()
	if sub == "" {
		t.Fatal("SubstitutionCite empty — mutation that drops the pin silently would pass")
	}
	if !strings.Contains(sub, "asked=grok") || !strings.Contains(sub, "minted=cursor") {
		t.Fatalf("SubstitutionCite = %q", sub)
	}
}

func TestT743SubstitutionCiteEmptyWhenNoDrop(t *testing.T) {
	p := MintProviderPick{Provider: "claude", Knob: KnobExplicit}
	if got := p.SubstitutionCite(); got != "" {
		t.Fatalf("no-drop SubstitutionCite = %q", got)
	}
	if strings.Contains(p.Cite(), "substituted:") {
		t.Fatalf("no-drop Cite names a substitution: %q", p.Cite())
	}
}

func TestT743WithSubstitutionKeepsFirstDrop(t *testing.T) {
	p := MintProviderPick{Provider: "claude"}.WithSubstitution("grok", "ineligible")
	p = p.WithSubstitution("cursor", "unsteerable")
	if p.AskedProvider != "grok" || p.DropReason != "ineligible" {
		t.Fatalf("second WithSubstitution clobbered first: %+v", p)
	}
}
