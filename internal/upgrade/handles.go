// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import "github.com/marcelocantos/claudia"

// FromRegistry builds upgrade handles from a live claudia registry.
// Alive reflects a live control session in *this* coordinator. PID and
// ConnectURL come from Grok connect-mode (claudia serve); zero/empty
// means stdio mode or never launched durable.
func FromRegistry(reg *claudia.Registry) []Handle {
	if reg == nil {
		return nil
	}
	defs := reg.List()
	out := make([]Handle, 0, len(defs))
	for _, d := range defs {
		h := Handle{
			Name:       d.Name,
			SessionID:  d.SessionID,
			WorkDir:    d.WorkDir,
			Provider:   string(d.Provider),
			ConnectURL: d.ConnectURL,
			PID:        d.ConnectPID,
		}
		if proc := reg.Get(d.Name); proc != nil {
			if proc.Alive() {
				h.Alive = true
			}
			// Prefer live process endpoint over persisted def (may be fresher).
			if u := proc.ConnectURL(); u != "" {
				h.ConnectURL = u
			}
			if p := proc.PID(); p > 0 {
				h.PID = p
			}
		}
		out = append(out, h)
	}
	return out
}

// ReattachPlan describes what the next coordinator should do with a
// prior snapshot. Process reattach is available when handles carry
// connect-mode endpoints; session reattach uses agents.json + session/load.
type ReattachPlan struct {
	// SessionIDs to resume (same conversation; may mint a new process).
	SessionIDs []string
	// ProcessReattachPossible is true when at least one handle has a
	// connect-mode URL (durable serve) for same-process reattach.
	ProcessReattachPossible bool
	// Residual is non-empty when process reattach is incomplete/unavailable.
	Residual string
}

// PlanReattach turns a snapshot into an explicit reattach plan.
func PlanReattach(snap *Snapshot) ReattachPlan {
	if snap == nil {
		return ReattachPlan{}
	}
	possible := false
	for _, a := range snap.Agents {
		if a.ConnectURL != "" && a.PID > 0 {
			possible = true
			break
		}
	}
	residual := ""
	if !possible {
		residual = ResidualConnectMode
	}
	return ReattachPlan{
		SessionIDs:              SessionIDs(snap),
		ProcessReattachPossible: possible,
		Residual:                residual,
	}
}
