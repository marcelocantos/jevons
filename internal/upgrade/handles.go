// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

func processReattachable(h Handle) bool {
	// Cursor ACP records a child PID but the transport is stdio — a
	// leftover PID is not adoptable (🎯T541.1). Only Grok connect-mode
	// (URL+PID) and Claude tmux windows survive a coordinator death.
	if h.ConnectURL != "" && h.PID > 0 {
		return true
	}
	return tmuxAdoptableWindow(h.TmuxWindowID)
}

// tmuxAdoptableWindow is a real tmux window/pane id (@N / %N).
// Codex/Cursor stamp WindowID as "codex-app-server-…" / "cursor-acp-…";
// copying that into TmuxWindowID made processReattachable true, so
// SIGHUP skipped Stop and the exclusive home never flushed (🎯T545.1.2).
func tmuxAdoptableWindow(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if id[0] != '@' && id[0] != '%' {
		return false
	}
	if len(id) < 2 {
		return false
	}
	for _, r := range id[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// adoptiveTmuxWindowID copies a live WindowID into the handle only when
// it is a tmux id. ACP labels stay off TmuxWindowID so snapshots cannot
// revive the skip-Stop hole.
func adoptiveTmuxWindowID(id string) string {
	if tmuxAdoptableWindow(id) {
		return strings.TrimSpace(id)
	}
	return ""
}

// ShouldStopOnUpgrade is true when the seat cannot be adopted after
// SIGHUP: leave Grok connect-mode and Claude tmux alone; stop Cursor
// (and any other stdio-owned handle) so the successor does not stack
// a second client on the same store.db.
func ShouldStopOnUpgrade(h Handle, hasProc bool) bool {
	if processReattachable(h) {
		return false
	}
	if hasProc {
		return true
	}
	return strings.EqualFold(h.Provider, "cursor") && h.SessionID != ""
}

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
			if w := adoptiveTmuxWindowID(proc.WindowID()); w != "" {
				h.TmuxWindowID = w
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
		if processReattachable(a) {
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
