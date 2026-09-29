// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"
	"time"
)

// 🎯T899: the owner presses harder than the overseer by default; profiles
// are config, can be tuned, and can opt out.
func TestT899DeliveryEscalationLadder(t *testing.T) {
	var zero DeliveryEscalationConfig
	if first, after, ok := zero.Ladder(EscalationOwner); !ok || first != "steer" || after != DefaultOwnerInterruptAfter {
		t.Fatalf("owner default = %q %s %v", first, after, ok)
	}
	if first, after, ok := zero.Ladder(EscalationOverseer); !ok || first != "steer" || after != DefaultOverseerInterruptAfter {
		t.Fatalf("overseer default = %q %s %v", first, after, ok)
	}
	if _, _, ok := zero.Ladder("agent"); ok {
		t.Fatal("a sender without a profile must wait for the turn boundary")
	}
	tuned := DeliveryEscalationConfig{
		Owner:    EscalationProfile{First: "submit", InterruptAfterSeconds: 30},
		Overseer: EscalationProfile{InterruptAfterSeconds: -1},
	}
	if first, after, ok := tuned.Ladder(EscalationOwner); !ok || first != "submit" || after != 30*time.Second {
		t.Fatalf("tuned owner = %q %s %v", first, after, ok)
	}
	if first, after, ok := tuned.Ladder(EscalationOverseer); !ok || first != "steer" || after != 0 {
		t.Fatalf("never-interrupt overseer = %q %s %v", first, after, ok)
	}
	if _, _, ok := (DeliveryEscalationConfig{Owner: EscalationProfile{First: "queue"}}).Ladder(EscalationOwner); ok {
		t.Fatal("first: queue opts the class out")
	}
	if _, _, ok := (DeliveryEscalationConfig{Disabled: true}).Ladder(EscalationOwner); ok {
		t.Fatal("disabled opts every class out")
	}
}
