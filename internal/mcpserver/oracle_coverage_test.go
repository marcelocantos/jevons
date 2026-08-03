// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

// 🎯T31.2: pure oracle-coverage map (pinned / fuzzy / examples).
func TestCoverageMapSpiralAndProductionGate(t *testing.T) {
	m := NewCoverageMap("widget design")
	if m.ProductionBlocked() {
		t.Fatal("empty map is not production-blocked by fuzzy list (no fuzzy ids)")
	}
	if SpiralAllowsProductionClaims(SpiralDesign, m) {
		t.Fatal("empty map must not allow production claims")
	}

	if err := m.AddFuzzy("auth", "session cookie rules"); err != nil {
		t.Fatal(err)
	}
	if !m.ProductionBlocked() {
		t.Fatal("fuzzy region must block production")
	}
	if m.AllowsProduction("auth") {
		t.Fatal("fuzzy auth must refuse production")
	}

	// Cannot pin without examples (Goodhart: drive load-bearing examples).
	if err := m.Pin("auth", "go test ./auth -run Cookie"); err == nil {
		t.Fatal("pin without examples must fail")
	}

	ex := LoadBearingExample{When: "cookie missing", Expect: "401 Unauthorized"}
	if err := m.AddExample("auth", ex); err != nil {
		t.Fatal(err)
	}
	if err := m.Pin("auth", "go test ./auth -run Cookie"); err != nil {
		t.Fatal(err)
	}
	if m.ProductionBlocked() {
		t.Fatal("pinned-only map must not block")
	}
	if !m.AllowsProduction("auth") {
		t.Fatal("pinned auth allows production")
	}
	if !SpiralAllowsProductionClaims(SpiralNewOracle, m) {
		t.Fatal("pinned map allows production claims")
	}

	// Taste residue is not agent production work.
	if err := m.AddFuzzy("chrome", "panel chrome polish"); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkTaste("chrome"); err != nil {
		t.Fatal(err)
	}
	if m.AllowsProduction("chrome") {
		t.Fatal("taste region is owner gate, not production")
	}
	// Fuzzy list empty once taste/spike — only fuzzy blocks ProductionBlocked.
	if m.ProductionBlocked() {
		t.Fatalf("taste should not appear as fuzzy; fuzzy=%v", m.FuzzyIDs())
	}

	// Spike: exploratory, not production.
	if err := m.AddFuzzy("spike1", "layout experiment"); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkSpike("spike1"); err != nil {
		t.Fatal(err)
	}
	if m.AllowsProduction("spike1") {
		t.Fatal("spike refuses production claims")
	}

	md := m.SummaryMarkdown()
	for _, want := range []string{
		"Oracle-coverage map",
		"widget design",
		"[pinned]",
		"[taste]",
		"[spike]",
		"when **cookie missing** expect **401 Unauthorized**",
		"go test ./auth -run Cookie",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("SummaryMarkdown missing %q\n%s", want, md)
		}
	}
}

func TestCoverageMapErrors(t *testing.T) {
	m := NewCoverageMap("t")
	if err := m.AddFuzzy("", "x"); err == nil {
		t.Fatal("empty id")
	}
	if err := m.AddFuzzy("a", "A"); err != nil {
		t.Fatal(err)
	}
	if err := m.AddFuzzy("a", "dup"); err == nil {
		t.Fatal("dup id")
	}
	if err := m.AddExample("missing", LoadBearingExample{When: "x", Expect: "y"}); err == nil {
		t.Fatal("unknown region")
	}
	if err := m.AddExample("a", LoadBearingExample{}); err == nil {
		t.Fatal("empty example")
	}
	if err := m.Pin("a", ""); err == nil {
		t.Fatal("empty oracle hint after examples still needs hint; also needs examples")
	}
}

func TestSpiralNextCycle(t *testing.T) {
	p := SpiralDesign
	want := []string{"thin_slice", "owner_react", "intent_sharpen", "new_oracle", "design"}
	for _, w := range want {
		p = SpiralNext(p)
		if p.String() != w {
			t.Fatalf("got %s want %s", p.String(), w)
		}
	}
}

func TestRegionStatusAndIntentClassString(t *testing.T) {
	if RegionFuzzy.String() != "fuzzy" || RegionPinned.String() != "pinned" {
		t.Fatal(RegionFuzzy.String(), RegionPinned.String())
	}
	if IntentDecidable.String() != "decidable" || IntentTaste.String() != "taste" {
		t.Fatal(IntentDecidable.String())
	}
	if SpiralDesign.String() != "design" {
		t.Fatal(SpiralDesign.String())
	}
}

// 🎯T31.2 decidable-from-taste sort.
func TestClassifyDesignClause(t *testing.T) {
	cases := []struct {
		in   string
		want IntentClass
	}{
		{"", IntentAmbiguous},
		{"when cookie missing, expect 401", IntentDecidable},
		{"must return 200 on health check", IntentDecidable},
		{"it should feel delightful and polished", IntentTaste},
		{"I'll know it when I see it", IntentTaste},
		{"make it nice", IntentAmbiguous},
		// mixed → force split (ambiguous)
		{"when login fails expect 403 and it should feel nice", IntentAmbiguous},
	}
	for _, tc := range cases {
		got := ClassifyDesignClause(tc.in)
		if got != tc.want {
			t.Errorf("ClassifyDesignClause(%q)=%s want %s", tc.in, got, tc.want)
		}
	}
}

func TestParseLoadBearingExample(t *testing.T) {
	ex, ok := ParseLoadBearingExample("When cookie is missing, expect 401 Unauthorized.")
	if !ok {
		t.Fatal("expected parse")
	}
	if strings.ToLower(ex.When) != "cookie is missing" {
		t.Fatalf("when=%q", ex.When)
	}
	if strings.ToLower(ex.Expect) != "401 unauthorized" {
		t.Fatalf("expect=%q", ex.Expect)
	}
	if ex.Source != "owner" {
		t.Fatalf("source=%q", ex.Source)
	}

	ex2, ok := ParseLoadBearingExample("when user is admin expect full fleet tree")
	if !ok || ex2.When != "user is admin" || ex2.Expect != "full fleet tree" {
		t.Fatalf("got %+v ok=%v", ex2, ok)
	}
	if _, ok := ParseLoadBearingExample("just a vibe check"); ok {
		t.Fatal("no when/expect")
	}
}

func TestNilMapSafety(t *testing.T) {
	var m *CoverageMap
	if err := m.AddFuzzy("a", "b"); err == nil {
		t.Fatal("nil map")
	}
	if m.AllowsProduction("a") {
		t.Fatal("nil")
	}
	if m.SummaryMarkdown() != "" {
		t.Fatal("nil summary")
	}
	if SpiralAllowsProductionClaims(SpiralNewOracle, nil) {
		t.Fatal("nil map spiral")
	}
}
