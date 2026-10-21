// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// RevivePlanAuthPeers relaunches seats that broke on provider's plan login,
// under the fleet intent's revive gate. skip names a seat already handled.
func (s *Server) RevivePlanAuthPeers(provider claudia.Provider, skip string) []fleet.PlanAuthRevival {
	if s == nil || s.registry == nil {
		return nil
	}
	return fleet.RevivePlanAuthPeers(s.registry, s.fleetIntent(), provider, skip, time.Now())
}

// RevivePlanAfterOwnerReauth relaunches the seats an owner's successful
// reauth of provider's plan brings back (🎯T905), under the revive gate.
func (s *Server) RevivePlanAfterOwnerReauth(provider claudia.Provider, skip string) []fleet.PlanAuthRevival {
	if s == nil || s.registry == nil {
		return nil
	}
	return fleet.RevivePlanAfterOwnerReauth(s.registry, s.fleetIntent(), provider, skip, time.Now())
}

// RevivePlanAuthWhereHealthy relaunches auth-broken seats on every plan
// that has a running seat, the evidence that its login works again.
func (s *Server) RevivePlanAuthWhereHealthy() []fleet.PlanAuthRevival {
	if s == nil || s.registry == nil {
		return nil
	}
	return fleet.RevivePlanAuthWhereHealthy(s.registry, s.fleetIntent(), time.Now())
}

// ReattachRunningSeats re-adopts auto-start seats the broker still runs but
// this host lost its handle for, under the intent revive gate. Seats lost to
// a broker restart are relaunched, on a fresh session when they have no
// history, and one that stays down is reported (🎯T925 / 🎯T935).
func (s *Server) ReattachRunningSeats() []string {
	if s == nil || s.registry == nil {
		return nil
	}
	now := time.Now()
	res := fleet.ReattachRunningSeats(s.registry, s.fleetIntent(), now, s.relaunchableLostSeat)
	for _, name := range res.Back {
		// Running again: the broker stop no longer describes it, and must
		// not license a later relaunch of a seat stopped on purpose.
		s.seatStops().Forget(name)
	}
	s.noteBrokerLostOutcome(res, now)
	return res.Back
}

// relaunchableLostSeat reports whether the reattach loop may relaunch a seat
// it could not adopt. Only a seat lost to a broker restart qualifies, and an
// auto-spawned worker the frontier pause or an achieved target holds never
// does (🎯T1067, 🎯T1066): it is adopted if its process still runs, never
// started again. Relaunching
// the -auto seats a restart had lost is the launch storm the PO answered on
// 2026-10-12 by stopping the daemon with SIGSTOP.
func (s *Server) relaunchableLostSeat(name string) bool {
	if hold := s.autoSeatHold(name); hold != "" {
		s.noteRevive(name, "reattach", hold, nil)
		return false
	}
	return s.lostToBroker(name)
}
