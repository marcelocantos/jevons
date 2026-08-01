// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
)

// DeadAgentReport is one silent-death finding (🎯T85).
type DeadAgentReport struct {
	Name      string
	Recovered bool
	Error     string
}

// deadRecoveryPlan is the pure policy for a single agent (hermetic oracle).
// hasProc && !alive ⇒ detect. autoStart ⇒ try recover (Launch); else clear handle.
func deadRecoveryPlan(hasProc, alive, autoStart bool) (detect, tryRecover, clearHandle bool) {
	if !hasProc || alive {
		return false, false, false
	}
	if autoStart {
		return true, true, false // clear only if recover fails (caller)
	}
	return true, false, true
}

// SweepDeadAgents detects fleet agents whose process handle is present
// but no longer Alive (silent death without Stop). Recovery policy:
//   - AutoStart durable agents: re-Launch (rehydrate session)
//   - Others: Stop to clear the dead handle so status becomes "stopped"
//
// overseerName is never recovered here (owner chat overseer has its own path).
// Returns every detected dead name; Recovered true when Launch succeeded.
func SweepDeadAgents(reg *claudia.Registry, overseerName string) []DeadAgentReport {
	if reg == nil {
		return nil
	}
	var out []DeadAgentReport
	for _, d := range reg.List() {
		if d.Name == "" || d.Name == overseerName {
			continue
		}
		proc := reg.Get(d.Name)
		hasProc := proc != nil
		alive := hasProc && proc.Alive()
		detect, tryRecover, clearHandle := deadRecoveryPlan(hasProc, alive, d.AutoStart)
		if !detect {
			continue
		}
		rep := DeadAgentReport{Name: d.Name}
		if tryRecover {
			if _, err := reg.Launch(d.Name); err != nil {
				rep.Error = err.Error()
				reg.Stop(d.Name)
				slog.Warn("fleet health: dead AutoStart agent re-launch failed",
					"name", d.Name, "err", err)
			} else {
				rep.Recovered = true
				slog.Info("fleet health: re-launched dead AutoStart agent", "name", d.Name)
			}
		} else if clearHandle {
			reg.Stop(d.Name)
			slog.Info("fleet health: cleared dead non-AutoStart agent handle", "name", d.Name)
		}
		out = append(out, rep)
	}
	return out
}

// FormatDeadAgentReport is a one-line human summary for MCP / notify.
func FormatDeadAgentReport(reps []DeadAgentReport) string {
	if len(reps) == 0 {
		return "fleet health: no dead agents"
	}
	var parts []string
	for _, r := range reps {
		if r.Recovered {
			parts = append(parts, fmt.Sprintf("%s:recovered", r.Name))
		} else if r.Error != "" {
			parts = append(parts, fmt.Sprintf("%s:fail(%s)", r.Name, r.Error))
		} else {
			parts = append(parts, fmt.Sprintf("%s:stopped", r.Name))
		}
	}
	return "fleet health: dead agents → " + strings.Join(parts, ", ")
}
