// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T1139: the first relaunch after seat-gone failing on grant_held must not
// be the last — the bounded broker-lost cadence keeps trying.
func TestT1139GrantHeldDoesNotStopBrokerLostRetries(t *testing.T) {
	const name = "jv-t1139-grant-held"
	t935Cleanup(t, name)
	grantErr := errors.New(`grant_held (name="jv-t1139-grant-held"): grant jv-t1139-grant-held is owned by another connection`)
	reg := &t935Reg{
		defs:  []claudia.AgentDef{{Name: name, Purpose: claudia.PurposeWork}},
		alive: map[string]bool{},
		relaunch: func(string) (*LostSession, error) {
			return nil, grantErr
		},
	}
	lost := func(n string) bool { return n == name }
	start := time.Date(2026, 10, 21, 23, 31, 8, 0, time.UTC)
	for now := start; now.Before(start.Add(90 * time.Second)); now = now.Add(brokerLostRetry) {
		reattachWith(reg, fleetintent.Snapshot{}, now, lost, nil)
	}
	if reg.relaunchs < 3 {
		t.Fatalf("relaunch attempts = %d, want continued retries after grant_held", reg.relaunchs)
	}
	reg.relaunch = func(string) (*LostSession, error) { return nil, nil }
	if res := reattachWith(reg, fleetintent.Snapshot{}, start.Add(2*time.Minute), lost, nil); len(res.Back) != 1 {
		t.Fatalf("seat did not come back after grant released: %+v", res)
	}
}
