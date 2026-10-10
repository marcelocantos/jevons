// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownercomms

import (
	"github.com/marcelocantos/jevons/internal/silentresponse"
	"testing"
)

func TestFiveWayAdmissionAndPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		evidence Evidence
		want     Category
	}{
		{"routine worker completed", Evidence{}, Routine},
		{"owner only choice", Evidence{OwnerOnlyDecision: true}, OwnerDecision},
		{"new uncontained incident", Evidence{MaterialNewUncontainedAnomaly: true, AnomalyID: "inc-1"}, NewAnomaly},
		{"no identity cannot establish novelty", Evidence{MaterialNewUncontainedAnomaly: true}, Routine},
		{"known incident", Evidence{MaterialNewUncontainedAnomaly: true, AnomalyID: "inc-1", DeliveredIDs: map[string]bool{"inc-1": true}}, Routine},
		{"direct answer", Evidence{DirectOwnerQuestion: true}, DirectAnswer},
		{"explicitly requested status", Evidence{OwnerRequestedStatus: true}, AskedStatus},
		{"decision before incident", Evidence{OwnerOnlyDecision: true, MaterialNewUncontainedAnomaly: true, AnomalyID: "inc-2"}, OwnerDecision},
		{"incident before answer", Evidence{MaterialNewUncontainedAnomaly: true, AnomalyID: "inc-2", DirectOwnerQuestion: true}, NewAnomaly},
		{"duplicate does not swallow question", Evidence{MaterialNewUncontainedAnomaly: true, AnomalyID: "inc-1", DeliveredIDs: map[string]bool{"inc-1": true}, DirectOwnerQuestion: true}, DirectAnswer},
		{"question before status", Evidence{DirectOwnerQuestion: true, OwnerRequestedStatus: true}, DirectAnswer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.evidence)
			if got != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
			text := Response(got, "candidate acknowledgment")
			if got == Routine {
				if text != "[silent]" || !silentresponse.Is(text) {
					t.Fatalf("routine leaked: %q", text)
				}
			} else if text != "candidate acknowledgment" {
				t.Fatalf("send lost: %q", text)
			}
		})
	}
	if Category("unknown").Send() {
		t.Fatal("unknown category must not authorize a send")
	}
}
