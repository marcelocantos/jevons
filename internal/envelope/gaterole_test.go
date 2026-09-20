// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"
	"testing"
)

func TestGateRoleRoundTrip(t *testing.T) {
	m := validFinish()
	m.GateRoles = []GateRole{
		{ID: "1ba897f8", Role: GateRoleControl},
		{ID: "453cc1d9", Role: GateRoleControl},
	}
	raw := Format(m)
	if !strings.Contains(raw, "jevons: gate-role 1ba897f8 control") {
		t.Fatalf("missing first gate-role in:\n%s", raw)
	}
	if !strings.Contains(raw, "jevons: gate-role 453cc1d9 control") {
		t.Fatalf("missing second gate-role in:\n%s", raw)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, raw)
	}
	if len(got.GateRoles) != 2 {
		t.Fatalf("GateRoles=%v", got.GateRoles)
	}
	if got.GateRoles[0].ID != "1ba897f8" || !got.GateRoles[0].IsControl() {
		t.Fatalf("first=%+v", got.GateRoles[0])
	}
	if got.GateRoles[1].ID != "453cc1d9" || !got.GateRoles[1].IsControl() {
		t.Fatalf("second=%+v", got.GateRoles[1])
	}
}

func TestParseGateRoleForms(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		id   string
		ctrl bool
	}{
		{"1ba897f8 control", "1ba897f8", true},
		{"id=1ba897f8 role=control", "1ba897f8", true},
		{"id=453cc1d9 before", "453cc1d9", true},
		{"abc inherited", "abc", false},
	} {
		r, err := parseGateRole(tc.raw)
		if err != nil {
			t.Fatalf("parseGateRole(%q): %v", tc.raw, err)
		}
		if r.ID != tc.id || r.IsControl() != tc.ctrl {
			t.Fatalf("parseGateRole(%q)=%+v, want id=%s control=%v", tc.raw, r, tc.id, tc.ctrl)
		}
	}
}

func TestControlIDsReadsSlotsOutsideALine1Fence(t *testing.T) {
	// The ge-t191 specimen opened with prose, then a fence. A checker that
	// only Parse()s line-1 envelopes would miss a declared role there.
	text := strings.Join([]string{
		"Work is complete on a branch.",
		"",
		"```jevons",
		"jevons: kind finish-report",
		"jevons: target T191",
		"jevons: oracle sha=7d68797 gate-id=59dc1780",
		"jevons: gate-role 1ba897f8 control",
		"jevons: silent-ledger none",
		"```",
		"",
		"The before-gate is cited below.",
	}, "\n")
	ids := ControlIDs(text)
	if !ids["1ba897f8"] {
		t.Fatalf("ControlIDs=%v, want 1ba897f8", ids)
	}
	if m, _ := Parse(text); m != nil {
		t.Fatal("fence is not at line 1; Parse must not treat this as an envelope")
	}
}

func TestParseGateRoleRequiresIDAndRole(t *testing.T) {
	if _, err := parseGateRole("1ba897f8"); err == nil {
		t.Fatal("id without role must fail")
	}
	if _, err := parseGateRole(""); err == nil {
		t.Fatal("empty must fail")
	}
}
