// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/delivery"
)

// 🎯T899: escalation bookkeeping events do not paint a phase; a busy agent
// that took a steered message is still whatever it was.
func TestT899EscalationEventsAreNotAPhase(t *testing.T) {
	for _, pt := range []string{delivery.ProgressDeliveryAbsorbed, delivery.ProgressDeliveryEscalated} {
		if s, ok := phaseFromEvent(claudia.Event{Type: "progress", ProgressType: pt, Text: "status?"}); ok {
			t.Fatalf("%s painted phase %+v", pt, s)
		}
	}
	if progressTypeDeliveryAbsorbed != delivery.ProgressDeliveryAbsorbed || progressTypeDeliveryEscalated != delivery.ProgressDeliveryEscalated {
		t.Fatal("phase mapper spells the claudia progress types differently")
	}
}
