// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T171 dual-path post-restart recovery (builds on 🎯T207 event path):
//
//  1. daemon-restarted → each durable parent PO + overseer (reattached summary)
//  2. short fire-and-forget resume → open-mission work agents only (T207 brief-or-verify path)
//  3. worker-idle → parent PO only on working→idle transition (debounced; not a poll)
//
// Rejected: only-PO hope, only-blast-everyone. Mechanical floor (dead/stuck) stays T204/T85.

const (
	// DefaultWorkerIdleDebounce avoids double-firing on flappy end_turn edges.
	DefaultWorkerIdleDebounce = 15 * time.Second
	// DefaultDaemonRestartNotifyDelay lets StartAll + wire settle.
	DefaultDaemonRestartNotifyDelay = 12 * time.Second

	eventWorkerIdle      = "worker-idle"
	eventDaemonRestarted = "daemon-restarted"
	defaultProductPOName = "jevons-po"
)

// WorkerIdleRef is one work agent mentioned in an idle/restart event body.
// SilentResponsePrefix / IsSilentAgentResponse live in silent_response.go (single definition).
type WorkerIdleRef struct {
	Name     string
	Parent   string
	TargetID string
	Purpose  string
	Phase    string
	Status   string // process status hint: running | stopped
}

// ShouldEmitWorkerIdle is the hermetic gate for enter-idle events.
// nextPhase must be idle; prevPhase must have been working (real turn),
// not seed-idle or empty (daemon boot seed is not an "enter idle" edge).
func ShouldEmitWorkerIdle(prevPhase, nextPhase, purpose string, openMission bool) bool {
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose == claudia.PurposeOverseer || purpose == claudia.PurposeAside {
		return false
	}
	if !openMission {
		return false
	}
	next := strings.ToLower(strings.TrimSpace(nextPhase))
	prev := strings.ToLower(strings.TrimSpace(prevPhase))
	if next != "idle" {
		return false
	}
	// Only fire on working → idle (turn actually ended).
	return prev == "working"
}

// ResolveEventParent picks who receives the idle/restart signal.
// Prefer AgentDef.Parent; empty → defaultPO (jevons-po); never the worker itself.
func ResolveEventParent(def claudia.AgentDef, defaultPO, overseer string) string {
	p := strings.TrimSpace(def.Parent)
	if p == "" {
		p = strings.TrimSpace(defaultPO)
	}
	if p == "" {
		p = defaultProductPOName
	}
	if p == def.Name {
		// Self-parented oddity — escalate to overseer if different.
		if overseer != "" && overseer != def.Name {
			return overseer
		}
		return defaultProductPOName
	}
	return p
}

// FormatWorkerIdleText builds the fire-and-forget body for a parent PO
// when a worker transitions into idle with open mission.
func FormatWorkerIdleText(w WorkerIdleRef) string {
	var b strings.Builder
	b.WriteString("A work agent under you entered phase=idle after generating (turn ended) ")
	b.WriteString("while it still looks like open mission work.\n\n")
	fmt.Fprintf(&b, "Worker: %s\n", strings.TrimSpace(w.Name))
	if tid := strings.TrimSpace(strings.TrimPrefix(w.TargetID, "🎯")); tid != "" {
		fmt.Fprintf(&b, "Target: 🎯%s\n", tid)
	} else {
		b.WriteString("Target: (none bound — still treat as open work unless you know it is done)\n")
	}
	if p := strings.TrimSpace(w.Parent); p != "" {
		fmt.Fprintf(&b, "Parent: %s\n", p)
	}
	b.WriteString("\nThis is the same class as a CLI session that stops after interrupt/restart ")
	b.WriteString("and needs an explicit continue — not a backend outage.\n")
	b.WriteString("Act: continue / re-brief / restart that worker / reap if done / file 🎯 if product gap.\n")
	b.WriteString("Do not wait for the owner to babysit. Local master only (T104) unless Ship was opened.\n")
	b.WriteString(silentResponseInstruction)
	return b.String()
}

// FormatDaemonRestartedText builds one parent- or overseer-facing restart brief
// listing reattached work children (name, target_id, status/phase).
func FormatDaemonRestartedText(parent string, workers []WorkerIdleRef) string {
	var b strings.Builder
	b.WriteString("jevonsd restarted (or reattached). Control plane is back.\n\n")
	b.WriteString("Hint: reattached sessions may be phase=idle (status=running) until they ")
	b.WriteString("receive a turn — not a default 'restart your workers' (process often fine). ")
	b.WriteString("Same class as resuming a CLI after Ctrl+C: explicit continue/re-brief, not more waiting.\n\n")
	if parent != "" {
		fmt.Fprintf(&b, "Addressed to: %s\n", parent)
	}
	if len(workers) == 0 {
		b.WriteString("No running work children listed under you right now.\n")
	} else {
		b.WriteString("Reattached work agents (name, target, status):\n")
		for _, w := range workers {
			tid := strings.TrimSpace(strings.TrimPrefix(w.TargetID, "🎯"))
			st := strings.TrimSpace(w.Status)
			if st == "" {
				st = "running"
			}
			ph := strings.TrimSpace(w.Phase)
			if ph == "" {
				ph = "idle"
			}
			if tid != "" {
				fmt.Fprintf(&b, "  - %s | 🎯%s | status=%s phase=%s\n", w.Name, tid, st, ph)
			} else {
				fmt.Fprintf(&b, "  - %s | status=%s phase=%s\n", w.Name, st, ph)
			}
		}
	}
	b.WriteString("\nDual path (🎯T171): framework also short-resumes open-mission workers; ")
	b.WriteString("you replan/reap/reseed. Do not blast missionless/aside/deliberate-stop. ")
	b.WriteString("Local master only (T104).\n")
	b.WriteString(silentResponseInstruction)
	return b.String()
}

// DaemonRestartEventTargets returns who should receive daemon-restarted:
// each parent key in byParent (durable POs / bosses) plus overseer when distinct.
// Pure helper for hermetic emit-target oracles (🎯T171).
func DaemonRestartEventTargets(byParent map[string][]WorkerIdleRef, overseer, defaultPO string) []string {
	if overseer == "" {
		overseer = "jevons"
	}
	if defaultPO == "" {
		defaultPO = defaultProductPOName
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(byParent) == 0 {
		add(defaultPO)
	} else {
		for parent := range byParent {
			// Parent keys that are the overseer still count if they own work
			// children; we always add overseer once below for cockpit.
			if parent == overseer {
				continue // overseer added explicitly as cockpit recipient
			}
			add(parent)
		}
		if len(out) == 0 {
			add(defaultPO)
		}
	}
	add(overseer) // always: cockpit dual-path (1)
	return out
}

// EligibleOpenMissionResume is the pure gate for post-restart short resume
// (🎯T171 path 2). True only for open-mission work implementers — not aside,
// overseer, PO/boss coordinators, deliberate-stop, design-gated, or finished.
//
// Open mission: purpose=work AND process running AND (AutoStart OR bound
// target_id) AND not the durable PO/boss name heuristic.
func EligibleOpenMissionResume(d claudia.AgentDef, running, deliberateStop, designGated, looksFinished bool) bool {
	if deliberateStop || designGated || looksFinished {
		return false
	}
	if !running {
		return false
	}
	purpose := strings.TrimSpace(d.Purpose)
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose == claudia.PurposeOverseer || purpose == claudia.PurposeAside {
		return false
	}
	if purpose != claudia.PurposeWork {
		return false
	}
	// POs/bosses get daemon-restarted (path 1), not implementer short-resume.
	if looksLikePOOrBoss(d.Name) {
		return false
	}
	tid := strings.TrimSpace(d.TargetID)
	if tid == "" && !d.AutoStart {
		return false // missionless non-durable: do not blast
	}
	// AutoStart and/or bound target_id.
	return d.AutoStart || tid != ""
}

// CollectWorkChildren groups running purpose=work agents by ResolveEventParent.
// phaseByName is optional (name → phase); default phase is idle after reattach.
func CollectWorkChildren(defs []claudia.AgentDef, running func(name string) bool, defaultPO, overseer string) map[string][]WorkerIdleRef {
	return CollectWorkChildrenWithPhase(defs, running, nil, defaultPO, overseer)
}

// CollectWorkChildrenWithPhase is CollectWorkChildren with optional phase map.
func CollectWorkChildrenWithPhase(defs []claudia.AgentDef, running func(name string) bool, phaseByName func(name string) string, defaultPO, overseer string) map[string][]WorkerIdleRef {
	out := map[string][]WorkerIdleRef{}
	if running == nil {
		running = func(string) bool { return false }
	}
	for _, d := range defs {
		if d.Name == "" || d.Name == overseer {
			continue
		}
		purpose := d.Purpose
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		if purpose != claudia.PurposeWork {
			continue
		}
		if !running(d.Name) {
			continue
		}
		parent := ResolveEventParent(d, defaultPO, overseer)
		phase := "idle"
		if phaseByName != nil {
			if p := strings.TrimSpace(phaseByName(d.Name)); p != "" {
				phase = p
			}
		}
		ref := WorkerIdleRef{
			Name:     d.Name,
			Parent:   parent,
			TargetID: d.TargetID,
			Purpose:  purpose,
			Phase:    phase,
			Status:   "running",
		}
		out[parent] = append(out[parent], ref)
	}
	return out
}

// FlattenWorkChildren returns a de-duplicated list of all work refs in byParent.
func FlattenWorkChildren(byParent map[string][]WorkerIdleRef) []WorkerIdleRef {
	seen := map[string]bool{}
	var out []WorkerIdleRef
	for _, kids := range byParent {
		for _, w := range kids {
			if w.Name == "" || seen[w.Name] {
				continue
			}
			seen[w.Name] = true
			out = append(out, w)
		}
	}
	return out
}

// CountWorkChildren returns how many purpose=work agents list parentName as Parent.
// Used by the enter-idle open-mission gate (🎯T244) so unbound POs with zero
// work children are not treated as open mission.
func CountWorkChildren(defs []claudia.AgentDef, parentName string) int {
	parentName = strings.TrimSpace(parentName)
	if parentName == "" {
		return 0
	}
	n := 0
	for _, c := range defs {
		if strings.TrimSpace(c.Parent) != parentName {
			continue
		}
		purpose := strings.TrimSpace(c.Purpose)
		if purpose == "" {
			purpose = claudia.PurposeWork
		}
		if purpose == claudia.PurposeWork {
			n++
		}
	}
	return n
}

// HasOpenMissionForIdle is the default open-mission heuristic for enter-idle.
// purpose=work with a bound TargetID is open (callers may tighten with
// missionOpen). Unbound implementers stay open so the parent PO can reap.
// Unbound PO/boss-shaped agents with zero work children are NOT open mission
// (🎯T244) — long-lived standing idle must not thrash the overseer.
func HasOpenMissionForIdle(d claudia.AgentDef, missionOpen func(targetID string) bool, workChildCount int) bool {
	purpose := d.Purpose
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose != claudia.PurposeWork {
		return false
	}
	tid := strings.TrimSpace(d.TargetID)
	if tid == "" {
		// 🎯T244: unbound PO/boss with no work children = standing idle, not open mission.
		if looksLikePOOrBoss(d.Name) && workChildCount <= 0 {
			return false
		}
		return true // unbound implementer still visible to parent
	}
	if missionOpen != nil {
		return missionOpen(tid)
	}
	return true
}
