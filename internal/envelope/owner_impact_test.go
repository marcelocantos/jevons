// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "testing"

func TestParseOwnerImpact(t *testing.T) {
	cases := []struct {
		raw  string
		want OwnerImpact
		ok   bool
	}{
		{"", OwnerImpactNone, true},
		{"none", OwnerImpactNoneSlot, true},
		{"decision", OwnerImpactDecision, true},
		{"fault", OwnerImpactFault, true},
		{"plan-change", OwnerImpactPlanChange, true},
		{"plan_change", OwnerImpactPlanChange, true},
		{"Plan Change", OwnerImpactPlanChange, true},
		{"done", "", false},
		{"maybe", "", false},
	}
	for _, c := range cases {
		got, ok := ParseOwnerImpact(c.raw)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("ParseOwnerImpact(%q) = (%q, %v), want (%q, %v)", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestOwnerImpactDeclared(t *testing.T) {
	declared := map[OwnerImpact]bool{
		OwnerImpactNone:       false,
		OwnerImpactNoneSlot:   false,
		OwnerImpactDecision:   true,
		OwnerImpactFault:      true,
		OwnerImpactPlanChange: true,
	}
	for impact, want := range declared {
		if got := impact.Declared(); got != want {
			t.Fatalf("%q.Declared() = %v, want %v", impact, got, want)
		}
	}
}

// Finish-report round-trips the owner-impact slot through Parse/Format.
func TestFinishReportOwnerImpactSlotRoundTrips(t *testing.T) {
	text := "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T1122\n" +
		"jevons: oracle sha=abcdef0123456\n" +
		"jevons: owner-impact decision\n" +
		"jevons: silent-ledger none\n" +
		"```\n\nLanded."
	m, err := Parse(text)
	if m == nil || err != nil {
		t.Fatalf("Parse: %v %v", m, err)
	}
	if m.OwnerImpact != OwnerImpactDecision {
		t.Fatalf("OwnerImpact = %q, want decision", m.OwnerImpact)
	}
	out := Format(m)
	m2, err := Parse(out)
	if m2 == nil || err != nil {
		t.Fatalf("round-trip Parse: %v %v", m2, err)
	}
	if m2.OwnerImpact != OwnerImpactDecision {
		t.Fatalf("round-trip OwnerImpact = %q, want decision", m2.OwnerImpact)
	}
}

func TestFinishReportOwnerImpactUnknownValueIsSlotError(t *testing.T) {
	text := "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target T1122\n" +
		"jevons: oracle sha=abcdef0123456\n" +
		"jevons: owner-impact maybe\n" +
		"jevons: silent-ledger none\n" +
		"```\n\nLanded."
	m, err := Parse(text)
	if err == nil {
		t.Fatalf("expected a slot error for owner-impact maybe, got nil (m=%v)", m)
	}
	se, ok := AsSlotError(err)
	if !ok {
		t.Fatalf("err = %v, want a *SlotError", err)
	}
	if se.Slot != "owner-impact" {
		t.Fatalf("SlotError.Slot = %q, want owner-impact", se.Slot)
	}
}
