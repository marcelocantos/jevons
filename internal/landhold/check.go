// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package landhold

import (
	"fmt"
	"os"
	"strings"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/config"
)

// OverrideEnv names an explicit owner/PO escape hatch (non-empty reason).
const OverrideEnv = "JEVONS_LAND_HOLD_OVERRIDE"

// Refusal is a policy block with a worker-facing message.
type Refusal struct {
	Action Action
	Seat   string
	Hold   Hold
	Reason string
}

func (r Refusal) Error() string { return r.Message() }

func (r Refusal) Message() string {
	var what string
	switch r.Action {
	case ActionIntegrate:
		what = "cmd/integrate (landing on the shared clone)"
	case ActionRestart:
		what = "scripts/restart-jevonsd.sh (development daemon activation)"
	default:
		what = "this operation"
	}
	return fmt.Sprintf(
		"landhold: refusing %s for seat %q under an active no-merge/no-activation hold (%s). "+
			"Lift the hold, finish under PO integrate, or set %s to a named override reason (🎯T1040).",
		what, r.Seat, r.Reason, OverrideEnv)
}

func OpenDefault() (*Store, error) {
	stateDir := strings.TrimSpace(os.Getenv("JEVONS_STATE_DIR"))
	if stateDir == "" {
		cfg, err := config.Load(config.DefaultPath())
		if err == nil && strings.TrimSpace(cfg.StateDir) != "" {
			stateDir = cfg.StateDir
		} else {
			stateDir = config.Default().StateDir
		}
	}
	return Open(stateDir)
}

func OverrideReason() string {
	return strings.TrimSpace(os.Getenv(OverrideEnv))
}

func CheckSeat(st *Store, seat, override string, action Action) (*Refusal, error) {
	seat = strings.TrimSpace(seat)
	if seat == "" || override != "" {
		return nil, nil
	}
	store := st
	if store == nil {
		opened, err := OpenDefault()
		if err != nil {
			return nil, err
		}
		store = opened
	}
	hold := store.EffectiveHold(seat)
	if !hold.Blocks(action) {
		return nil, nil
	}
	return &Refusal{Action: action, Seat: seat, Hold: hold, Reason: describeHold(hold)}, nil
}

func describeHold(h Hold) string {
	var parts []string
	if h.NoMerge {
		parts = append(parts, "no-merge")
	}
	if h.NoActivation {
		parts = append(parts, "no-activation")
	}
	return strings.Join(parts, ",")
}

func SeatFromEnv() string {
	return strings.TrimSpace(os.Getenv(commitattrib.ActorEnv))
}
