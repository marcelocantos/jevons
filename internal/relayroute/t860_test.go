// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package relayroute

import "testing"

// 🎯T860: table-driven hermetic arms for disclaimer and externally attributed
// oracle markers. Integration with Classify/Reason is in classify_test.go
// (🎯T766.4: unenveloped prose stays parent; declared finish-report skips).
func TestT860OracleDoneTable(t *testing.T) {
	const specimen = "Not claiming done/complete — reporting status only. " +
		"For reference, jv-t852-mux-journal's commit aacadd88 reported GATE go-test-TestT852 exit=0 GREEN."

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "specimen-20260923-disclaimer-and-other-agent-gate",
			body: specimen,
			want: false,
		},
		{
			name: "disclaimer-not-claiming-done",
			body: "Not claiming done/complete — reporting status only. GATE x GREEN.",
			want: false,
		},
		{
			name: "disclaimer-no-assumed-completion",
			body: "no assumed completion. done. GATE foo GREEN.",
			want: false,
		},
		{
			name: "disclaimer-reporting-status-only",
			body: "reporting status only. complete. oracle sha=abc.",
			want: false,
		},
		{
			name: "attributed-another-agent",
			body: "Diagnosis done. Another agent's commit reported GATE x GREEN; that is not mine.",
			want: false,
		},
		{
			name: "attributed-sibling-commit",
			body: "finished. sibling's commit had GATE y GREEN.",
			want: false,
		},
		{
			name: "attributed-named-agent-commit",
			body: "done. jv-t852-mux-journal's commit aacadd88 GATE z GREEN.",
			want: false,
		},
		{
			name: "genuine-oracle-done-prose",
			body: "GATE x GREEN, tests pass, done.",
			want: true,
		},
		{
			name: "bare-done-no-oracle",
			body: "done.",
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := oracleDone(tc.body); got != tc.want {
				t.Fatalf("oracleDone=%v want %v", got, tc.want)
			}
		})
	}
}

func TestT860SpecimenReasonStaysParent(t *testing.T) {
	const specimen = "Not claiming done/complete — reporting status only. " +
		"For reference, jv-t852-mux-journal's commit aacadd88 reported GATE go-test-TestT852 exit=0 GREEN."
	if got := Reason(specimen); got != "parent" {
		t.Fatalf("Reason=%s want parent (stored report 20260923T054954Z-16fe87e7)", got)
	}
	if got := Classify(specimen); got != RouteParent {
		t.Fatalf("Classify=%s want parent", got)
	}
}
