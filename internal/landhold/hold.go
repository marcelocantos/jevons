// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package landhold enforces no-merge / no-activation mission holds (🎯T1040).
package landhold

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// Action is an operation the hold can block.
type Action int

const (
	ActionIntegrate Action = 1
	ActionRestart   Action = 2
)

// Hold names which operations are forbidden while the hold is active.
type Hold struct {
	NoMerge      bool
	NoActivation bool
}

// Active reports whether the hold blocks anything.
func (h Hold) Active() bool { return h.NoMerge || h.NoActivation }

// Blocks reports whether action is refused under this hold.
func (h Hold) Blocks(action Action) bool {
	switch action {
	case ActionIntegrate:
		return h.NoMerge
	case ActionRestart:
		return h.NoActivation
	default:
		return false
	}
}

// ParseHold reads the jevons: hold slot value (comma-separated tokens).
func ParseHold(raw string) (Hold, error) {
	e, err := envelope.ParseLandHold(raw)
	if err != nil {
		return Hold{}, err
	}
	return FromEnvelope(e), nil
}

// FromEnvelope converts a parsed `jevons: hold` slot into an enforceable Hold.
func FromEnvelope(e envelope.LandHold) Hold {
	return Hold{NoMerge: e.NoMerge, NoActivation: e.NoActivation}
}

// FormatHold renders a hold slot value.
func FormatHold(h Hold) string {
	var parts []string
	if h.NoMerge {
		parts = append(parts, "no-merge")
	}
	if h.NoActivation {
		parts = append(parts, "no-activation")
	}
	return strings.Join(parts, ",")
}
