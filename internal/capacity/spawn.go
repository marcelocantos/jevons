// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import "strings"

// SpawnKind is what kind of process jevons_agent_start is about to mint.
//
// 🎯T460 is not a second copy of ClassBuildMission. Open Build work is
// never shed (🎯T359): a worker already running a target keeps running
// at critical host pressure. A *new* pane is what melted the host on
// 2026-08-15 — the governor reported 100% headroom at load 304 and
// admitted every spawn. New worker panes are refused at critical; owner
// seats and control-plane repair are not.
type SpawnKind string

const (
	// SpawnWorker is a product worker / aside / PO child. Refused at
	// PressureCritical.
	SpawnWorker SpawnKind = "worker"
	// SpawnOwner is the owner-facing overseer seat. Never refused here.
	SpawnOwner SpawnKind = "owner"
	// SpawnControlRepair is a sentinel / watchdog / repair agent.
	// Never refused here — it is what unsticks the fleet.
	SpawnControlRepair SpawnKind = "control_repair"
)

// ClassifySpawnKind maps a jevons_agent_start (purpose, name) onto a
// SpawnKind. Unknown names are workers: failing open on the expensive
// path is how the host died.
func ClassifySpawnKind(purpose, name string) SpawnKind {
	p := strings.ToLower(strings.TrimSpace(purpose))
	n := strings.ToLower(strings.TrimSpace(name))
	if p == "overseer" || n == "jevons" {
		return SpawnOwner
	}
	switch {
	case strings.Contains(n, "sentinel"),
		strings.Contains(n, "watchdog"),
		strings.Contains(n, "supervise"):
		return SpawnControlRepair
	default:
		return SpawnWorker
	}
}

// BlocksUnattendedSpawn reports whether T155 / T193 / T325.1 must stop
// kicking. Critical host pressure is a blocking condition: "frontier is
// not empty" does not mean keep spawning on a host that cannot run what
// is already spawned (🎯T460 §3).
func BlocksUnattendedSpawn(a Assessment) bool {
	return a.Pressure == PressureCritical
}

// AdmitSpawn decides whether jevons_agent_start may mint a new process
// (🎯T460). Unlike Admit(ClassBuildMission), a new pane is load, not
// already-open work.
//
// Oracle shape: the 2026-08-15 numbers classify critical and refuse a
// worker spawn; the idle-host control admits; owner and control-repair
// stay admitted on the melted host. An over-broad fix that refuses
// everything fails the control.
func AdmitSpawn(kind SpawnKind, snap Snapshot, pol *Policy) Decision {
	if pol == nil {
		pol = DefaultPolicy()
	}
	a := Assess(snap, pol)
	d := Decision{Name: string(kind), Pressure: a.Pressure}
	switch kind {
	case SpawnOwner:
		d.Class = ClassOwnerTurn
		d.Verdict, d.Tier, d.Reason = VerdictAdmit, TierFull, ReasonOwnerPriority
		d.Detail = "owner-facing seats are never refused for host pressure (🎯T460)"
		return d
	case SpawnControlRepair:
		d.Class = ClassControlRepair
		d.Verdict, d.Tier, d.Reason = VerdictAdmit, TierFull, ReasonOwnerPriority
		d.Detail = "control-plane repair is never refused for host pressure (🎯T460)"
		return d
	default:
		d.Class = ClassBuildMission
		if BlocksUnattendedSpawn(a) {
			d.Verdict, d.Reason = VerdictDefer, deferReason(a, ReasonHostSaturated)
			d.Detail = "host pressure critical; refusing a new worker pane (🎯T460): " + joinReasons(a.Reasons)
			return d
		}
		if snap.SpawnHalted {
			d.Verdict, d.Reason = VerdictDefer, ReasonSpawnHalted
			d.Detail = "budget clamp has halted spawning (🎯T36)"
			return d
		}
		d.Verdict, d.Tier, d.Reason = VerdictAdmit, TierFull, ReasonHeadroomOK
		d.Detail = "host has room for a new worker pane"
		return d
	}
}

// AdmitSpawn is the governor seam over the pure policy, using the live
// snapshot. A nil receiver admits (unknown is not critical).
func (g *Governor) AdmitSpawn(kind SpawnKind, name string) Decision {
	if g == nil {
		return Decision{
			Name: name, Verdict: VerdictAdmit, Tier: TierFull,
			Reason: ReasonHeadroomOK, Detail: "no governor wired",
		}
	}
	pol := g.policy()
	g.mu.Lock()
	snap := g.snapshot()
	g.mu.Unlock()
	d := AdmitSpawn(kind, snap, pol)
	d.Name = name
	return d
}
