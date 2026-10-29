// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// providerPickInputs decides whether a start reuses the stored provider
// (resume) or resolves afresh across eligible dests (🎯T1148). A dead seat
// whose stored plan is mint-ineligible must not inherit that backend on
// remint; an empty stored provider on a preserved registry row must not
// fall through PickMintProvider with PlanDestOK=false.
func (s *Server) providerPickInputs(name, providerArg, stored string, rowExisted bool, ownerAsked bool) (storedOut string, pickExisted bool, droppedPinned string) {
	pickExisted = rowExisted
	storedOut = stored
	if strings.TrimSpace(providerArg) != "" || ownerAsked || !rowExisted || s == nil || s.registry == nil {
		return storedOut, pickExisted, ""
	}
	seatstate.ObserveRegistrySeat(s.registry, name)
	if s.seatState(name).LocalAlive == seatstate.Yes {
		return storedOut, pickExisted, ""
	}
	plan := strings.TrimSpace(seatprovider.Resolve(claudia.Provider(storedOut)).Plan.String())
	if plan == "" {
		return "", false, ""
	}
	if s.providerDestEligible(plan) {
		return storedOut, pickExisted, ""
	}
	return "", false, plan
}

func (s *Server) planDestEmptyRegisterErr(droppedPinned string) error {
	base := "plan dest empty: all published providers fail mint thresholds; refusing to land on a hot dest (🎯T390.1.5)"
	if strings.TrimSpace(droppedPinned) == "" {
		return fmt.Errorf("%s", base)
	}
	detail := s.pinnedProviderRefusalDetail(droppedPinned)
	if detail == "" {
		detail = "mint-ineligible"
	}
	return fmt.Errorf("%s; prior seat pinned %s (%s) — not the same as every published provider failing",
		base, droppedPinned, detail)
}

func (s *Server) pinnedProviderRefusalDetail(planName string) string {
	_, cands, now, th, ok := s.planPolicyInputs()
	if !ok {
		return ""
	}
	want := seatprovider.Resolve(claudia.Provider(planName)).Plan
	for _, c := range cands {
		if seatprovider.Resolve(claudia.Provider(c.Provider)).Plan != want {
			continue
		}
		return strings.TrimSpace(planusage.MintRefusalDetail([]planusage.DestCand{c}, now, th))
	}
	return ""
}
