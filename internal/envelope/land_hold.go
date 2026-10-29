// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"fmt"
	"strings"
)

// LandHold is the typed value of a `jevons: hold` slot (🎯T1040): which
// landing operations a mission forbids. internal/landhold enforces it. The
// token grammar lives here so slot parsing stays inside the envelope
// grammar the T766.4.1 call-graph ratchet exempts.
type LandHold struct {
	NoMerge      bool
	NoActivation bool
}

// Active reports whether the hold forbids anything.
func (h LandHold) Active() bool { return h.NoMerge || h.NoActivation }

// ParseLandHold parses a comma-separated hold value. An unknown token is an
// error, never ignored.
func ParseLandHold(raw string) (LandHold, error) {
	var h LandHold
	for _, tok := range strings.Split(raw, ",") {
		switch strings.TrimSpace(strings.ToLower(tok)) {
		case "":
			continue
		case "no-merge", "no_merge", "nomerge":
			h.NoMerge = true
		case "no-activation", "no_activation", "noactivation":
			h.NoActivation = true
		default:
			return LandHold{}, fmt.Errorf("unknown hold token %q", tok)
		}
	}
	return h, nil
}
