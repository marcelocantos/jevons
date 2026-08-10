// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
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

// fleetSweepReg is the seam SweepDeadAgents needs so hermetic tests can
// inject hasProc&&!Alive without a real OS process (🎯T85 oracle).
type fleetSweepReg interface {
	List() []claudia.AgentDef
	// ProcState returns whether a process handle exists and whether it is Alive.
	ProcState(name string) (hasProc, alive bool)
	Launch(name string) error
	Stop(name string)
}

// claudiaSweep adapts *claudia.Registry to fleetSweepReg.
type claudiaSweep struct{ reg *claudia.Registry }

func (c claudiaSweep) List() []claudia.AgentDef {
	if c.reg == nil {
		return nil
	}
	return c.reg.List()
}

func (c claudiaSweep) ProcState(name string) (hasProc, alive bool) {
	if c.reg == nil {
		return false, false
	}
	proc := c.reg.Get(name)
	if proc == nil {
		return false, false
	}
	return true, proc.Alive()
}

func (c claudiaSweep) Launch(name string) error {
	_, err := fleet.LaunchRecovering(c.reg, name)
	return err
}

func (c claudiaSweep) Stop(name string) {
	c.reg.Stop(name)
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
	return sweepDeadAgents(claudiaSweep{reg}, overseerName)
}

// sweepDeadAgents is the testable implementation (real path + hermetic fakes).
func sweepDeadAgents(reg fleetSweepReg, overseerName string) []DeadAgentReport {
	if reg == nil {
		return nil
	}
	var out []DeadAgentReport
	for _, d := range reg.List() {
		if d.Name == "" || d.Name == overseerName {
			continue
		}
		hasProc, alive := reg.ProcState(d.Name)
		detect, tryRecover, clearHandle := deadRecoveryPlan(hasProc, alive, d.AutoStart)
		if !detect {
			continue
		}
		rep := DeadAgentReport{Name: d.Name}
		if tryRecover {
			if err := reg.Launch(d.Name); err != nil {
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

// FormatDeadAgentReport is a one-line human summary for MCP / notify / UI.
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

// PrependFleetHealth surfaces recovery on tool results (agent_list / send),
// not only slog (🎯T85 overseer/UI-visible path).
func PrependFleetHealth(body string, reps []DeadAgentReport) string {
	if len(reps) == 0 {
		return body
	}
	line := FormatDeadAgentReport(reps)
	if body == "" {
		return line
	}
	return line + "\n" + body
}
