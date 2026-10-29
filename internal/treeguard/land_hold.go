// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"github.com/marcelocantos/jevons/internal/fleetgit"
	"github.com/marcelocantos/jevons/internal/landhold"
)

func (e *Env) decideLandHold(command string) (Decision, bool) {
	if !fleetgit.FleetSeat() {
		return Decision{}, false
	}
	action, ok := landhold.CommandAction(command)
	if !ok {
		return Decision{}, false
	}
	ref, err := landhold.CheckSeat(nil, landhold.SeatFromEnv(), landhold.OverrideReason(), action)
	if err != nil {
		return Decision{}, false
	}
	if ref == nil {
		return Decision{Verdict: Allow, Reason: "land-hold-clear"}, true
	}
	landhold.LogRefusal(ref, command)
	return Decision{Verdict: Deny, Reason: "land-hold", Message: ref.Message()}, true
}
