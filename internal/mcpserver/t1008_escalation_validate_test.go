// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/escalate"
)

// 🎯T1008.2: every ladder s.escalationLadder builds must satisfy claudia's
// own Escalation.Validate() — the shape jevons constructs is now the same
// type claudia climbs for real (SendEscalating), so an invalid ladder here
// is no longer caught only by a type mismatch; it would reach
// *claudia.Agent.SendEscalating and fail there instead, mid-delivery.
func TestT1008EscalationLaddersValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config.DeliveryEscalationConfig
	}{
		{"defaults", config.DeliveryEscalationConfig{}},
		{"owner submit first", config.DeliveryEscalationConfig{
			Owner: config.EscalationProfile{First: "submit"},
		}},
		{"owner never interrupts", config.DeliveryEscalationConfig{
			Owner: config.EscalationProfile{InterruptAfterSeconds: -1},
		}},
		{"overseer custom deadline", config.DeliveryEscalationConfig{
			Overseer: config.EscalationProfile{InterruptAfterSeconds: 5},
		}},
		{"owner queue disables", config.DeliveryEscalationConfig{
			Owner: config.EscalationProfile{First: "queue"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(t.TempDir(), nil, nil)
			s.SetDeliveryEscalation(tc.cfg)
			for _, class := range []string{config.EscalationOwner, config.EscalationOverseer} {
				ladder, ok := s.EscalationLadderFor(class)
				if !ok {
					continue // queue/disabled: no ladder to validate
				}
				if err := ladder.Validate(); err != nil {
					t.Fatalf("class %q ladder %+v fails claudia.Escalation.Validate(): %v", class, ladder, err)
				}
			}
		})
	}
}

// A hand-built ladder that breaks claudia's rules (After decreasing, or a
// later rung that is not interrupt) is rejected by Validate — the negative
// case claudia's own Escalation.Validate enforces, exercised here through
// jevons's Ladder alias so a regression that detaches the alias from
// claudia's type would surface as a compile error, not a silent pass.
func TestT1008EscalationLadderValidateRejectsBadShapes(t *testing.T) {
	tooLong := escalate.Ladder{}
	for i := 0; i < 5; i++ {
		tooLong = append(tooLong, escalate.Step{Mode: "interrupt", After: time.Duration(i) * time.Second})
	}
	if err := tooLong.Validate(); err == nil {
		t.Fatal("a 5-rung ladder exceeds claudia's bound and must fail Validate")
	}

	decreasing := escalate.Ladder{
		{Mode: "steer", After: 0},
		{Mode: "interrupt", After: 10 * time.Second},
		{Mode: "interrupt", After: 5 * time.Second},
	}
	if err := decreasing.Validate(); err == nil {
		t.Fatal("a ladder whose After decreases between rungs must fail Validate")
	}

	wrongLaterMode := escalate.Ladder{
		{Mode: "steer", After: 0},
		{Mode: "submit", After: time.Minute},
	}
	if err := wrongLaterMode.Validate(); err == nil {
		t.Fatal("a non-interrupt later rung must fail Validate")
	}
}
