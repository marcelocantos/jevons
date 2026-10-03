// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// DeadAgentReport is one silent-death finding (🎯T85).
type DeadAgentReport struct {
	Name      string
	Recovered bool
	// Removed is the 🎯T544 outcome: a dead work seat left the registry.
	Removed bool
	Error   string
	// Declined names the 🎯T414 intent that stopped this sweep from reviving
	// a dead handle (empty when intent allowed the recovery). The sweep still
	// reports the agent: the owner should see that a process is gone, just
	// not see the daemon start it again.
	Declined string
	// Cause is why the handle died, when the harness knew (🎯T925).
	Cause string
	// Detail, when set, is the handling path's own account of what it did;
	// otherwise the seat-stop record derives one from the flags above.
	Detail string
}

// deadRecoveryPlan is the pure policy for a single agent (hermetic oracle).
// hasProc && !alive ⇒ detect. autoStart ⇒ try recover (Launch); else clear
// handle — or, for a dead work seat, remove the row outright (🎯T544).
//
// remove is the 🎯T544 outcome: a non-AutoStart seat whose purpose is work
// (or unset, which the fleet reads as work) has no owner-visible reason to
// stay registered once its process is gone. Stopping it painted a "stopped"
// row in the fleet tree forever — the UI drops names absent from the feed,
// so the server keeping the row was the whole bug. Asides, product owners
// and the overseer keep the old clear-handle behaviour: their rows carry
// history the owner reopens.
//
// intentAllows is the 🎯T414 gate, and it is the whole difference between
// this sweep recovering a crash and this sweep resurrecting a park. AutoStart
// is a property of the *definition* — it says this agent is the kind that
// gets launched by StartAll — so before intent existed the sweep read a
// deliberately stopped AutoStart worker as a silent death every single pass.
// That is one of the three resurrection paths of 2026-08-10.
func deadRecoveryPlan(hasProc, alive, autoStart, intentAllows bool, purpose string) (detect, tryRecover, clearHandle, remove bool) {
	if !hasProc || alive {
		return false, false, false, false
	}
	if !intentAllows {
		// Detected and reported, but neither revived nor cleared: clearing
		// the handle is how a stopped row loses the evidence that its
		// process died, and the owner asked for this one to be down.
		return true, false, false, false
	}
	if autoStart {
		return true, true, false, false // clear only if recover fails (caller)
	}
	if fleet.DeadSeatRemovable(purpose) {
		return true, false, false, true
	}
	return true, false, true, false
}

// fleetSweepReg is the seam recoverDeadHandles needs so hermetic tests can
// inject hasProc&&!Alive without a real OS process (🎯T85 oracle).
type fleetSweepReg interface {
	List() []claudia.AgentDef
	// ProcState returns whether a process handle exists and whether it is Alive.
	ProcState(name string) (hasProc, alive bool)
	Launch(name string) error
	Stop(name string)
	// RemoveDeadSeat drops the row entirely (🎯T544 dead work seat).
	RemoveDeadSeat(name string) error
	// ExitCause is why name's dead handle died, when the harness knew.
	ExitCause(name string) string
}

// claudiaSweep adapts *claudia.Registry to fleetSweepReg. account may be
// nil: fleetlog's nil receiver still removes, just without the event.
type claudiaSweep struct {
	reg     *claudia.Registry
	account *fleetlog.Account
}

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
	st := seatstate.ReadRegistry(c.reg, name)
	return st.Alive.Known(), st.Alive == seatstate.Yes
}

func (c claudiaSweep) ExitCause(name string) string {
	if c.reg == nil {
		return ""
	}
	return fleet.ExitCause(c.reg.Get(name))
}

func (c claudiaSweep) Launch(name string) error {
	_, err := fleet.LaunchRecovering(c.reg, name)
	return err
}

func (c claudiaSweep) Stop(name string) {
	c.reg.Stop(name)
	seatstate.ObserveStopped(c.reg, name)
}

func (c claudiaSweep) RemoveDeadSeat(name string) error {
	c.reg.Stop(name)
	seatstate.ObserveStopped(c.reg, name)
	_, err := c.account.Remove(c.reg, name, fleetlog.Removal{
		Reason: fleetlog.ReasonDeadSeat,
		Detail: "work seat's process exited without a terminal report (🎯T544)",
	})
	return err
}

// recoverDeadHandles detects fleet agents whose process handle is present
// but no longer Alive (silent death without Stop). Recovery policy:
//   - AutoStart durable agents: re-Launch (rehydrate session)
//   - Work seats (purpose work/unset): Remove the row (🎯T544), accounted
//     through account (nil is tolerated — the removal still happens)
//   - Others: Stop to clear the dead handle so status becomes "stopped"
//
// overseerName is never recovered here (owner chat overseer has its own path).
// Returns every detected dead name; Recovered true when Launch succeeded.
func recoverDeadHandles(reg *claudia.Registry, account *fleetlog.Account, overseerName string, intent fleetintent.Snapshot) []DeadAgentReport {
	if reg == nil {
		return nil
	}
	return sweepDeadAgents(claudiaSweep{reg: reg, account: account}, overseerName, intent)
}

// sweepDeadAgents is the testable implementation (real path + hermetic fakes).
func sweepDeadAgents(reg fleetSweepReg, overseerName string, intent fleetintent.Snapshot) []DeadAgentReport {
	if reg == nil {
		return nil
	}
	var out []DeadAgentReport
	for _, d := range reg.List() {
		if d.Name == "" || d.Name == overseerName {
			continue
		}
		hasProc, alive := reg.ProcState(d.Name)
		dec := intent.Allow(d.Name, fleetintent.ControlRevive)
		detect, tryRecover, clearHandle, remove := deadRecoveryPlan(hasProc, alive, d.AutoStart, dec.Allow, d.Purpose)
		if !detect {
			continue
		}
		// Read before any Stop or Launch replaces the dead handle.
		rep := DeadAgentReport{Name: d.Name, Cause: reg.ExitCause(d.Name)}
		if !dec.Allow {
			rep.Declined = dec.Reason
			slog.Info("fleet health: dead handle left alone — intent says do not run",
				"name", d.Name, "intent", string(dec.Blocking), "reason", dec.Reason)
			out = append(out, rep)
			continue
		}
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
		} else if remove {
			if err := reg.RemoveDeadSeat(d.Name); err != nil {
				rep.Error = err.Error()
				reg.Stop(d.Name)
				slog.Warn("fleet health: dead work seat removal failed; handle cleared",
					"name", d.Name, "err", err)
			} else {
				rep.Removed = true
				slog.Info("fleet health: removed dead work seat", "name", d.Name)
			}
		} else if clearHandle {
			reg.Stop(d.Name)
			slog.Info("fleet health: cleared dead non-AutoStart agent handle", "name", d.Name)
		}
		out = append(out, rep)
	}
	return out
}

// notifyDeadAgents delivers one fleet-health line for this sweep's dead
// set. An empty set forgets death streaks so a later death of the same
// name is a new occurrence (🎯T717), not a T428 echo of the previous one.
func (s *Server) notifyDeadAgents(reps []DeadAgentReport) {
	if s == nil {
		return
	}
	if len(reps) == 0 {
		s.forgetDeadAgentStreaks()
		return
	}
	line := FormatDeadAgentReport(reps)
	slog.Info(line)
	s.notifyFleetHealth(s.deadAgentOccurrence(reps), line)
}

func (s *Server) forgetDeadAgentStreaks() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.deadAgentStreak = nil
	s.mu.Unlock()
}

// deadAgentOccurrence is the T717 discriminator for a dead-agent sweep.
// Each name keeps a generation that is stable across consecutive sweeps
// (list-call echoes) and increments when the name leaves the dead set
// and returns (a second death as stopped).
func (s *Server) deadAgentOccurrence(reps []DeadAgentReport) string {
	if s == nil || len(reps) == 0 {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deadAgentStreak == nil {
		s.deadAgentStreak = map[string]uint64{}
	}
	seen := map[string]struct{}{}
	parts := make([]string, 0, len(reps))
	for _, r := range reps {
		n := strings.TrimSpace(r.Name)
		if n == "" {
			continue
		}
		seen[n] = struct{}{}
		gen, ok := s.deadAgentStreak[n]
		if !ok {
			s.deadAgentGen++
			gen = s.deadAgentGen
			s.deadAgentStreak[n] = gen
		}
		parts = append(parts, fmt.Sprintf("%s#%d", n, gen))
	}
	for n := range s.deadAgentStreak {
		if _, ok := seen[n]; !ok {
			delete(s.deadAgentStreak, n)
		}
	}
	return strings.Join(parts, ",")
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
		} else if r.Removed {
			parts = append(parts, fmt.Sprintf("%s:removed", r.Name))
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
