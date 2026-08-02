// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import "github.com/marcelocantos/claudia"

// FromRegistry builds upgrade handles from a live claudia registry.
// Alive reflects a running process in *this* coordinator; PID is not
// available from claudia's public Agent API (residual — always 0).
func FromRegistry(reg *claudia.Registry) []Handle {
	if reg == nil {
		return nil
	}
	defs := reg.List()
	out := make([]Handle, 0, len(defs))
	for _, d := range defs {
		h := Handle{
			Name:      d.Name,
			SessionID: d.SessionID,
			WorkDir:   d.WorkDir,
			Provider:  string(d.Provider),
		}
		if proc := reg.Get(d.Name); proc != nil && proc.Alive() {
			h.Alive = true
		}
		out = append(out, h)
	}
	return out
}

// ReattachPlan describes what the next coordinator should do with a
// prior snapshot. Process reattach is residual; session reattach uses
// agents.json + session/load (conversation durability).
type ReattachPlan struct {
	// SessionIDs to resume (same conversation; may mint a new process).
	SessionIDs []string
	// ProcessReattachPossible is false until claudia connect-mode exists.
	ProcessReattachPossible bool
	// Residual is non-empty when process reattach is unavailable.
	Residual string
}

// PlanReattach turns a snapshot into an explicit reattach plan.
func PlanReattach(snap *Snapshot) ReattachPlan {
	if snap == nil {
		return ReattachPlan{}
	}
	return ReattachPlan{
		SessionIDs:              SessionIDs(snap),
		ProcessReattachPossible: false,
		Residual:                ResidualConnectMode,
	}
}
