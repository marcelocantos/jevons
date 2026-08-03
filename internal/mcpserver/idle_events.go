// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T207 (event-first): framework detects; PO judges.
//
//   - worker-idle: only on transition into idle with open mission
//   - daemon-restarted: once per parent PO after jevonsd StartAll settle
//   - No blast "continue" to every agent; no exponential backoff ladder.
//
// Optional mechanical floor (dead process, stuck-busy) stays on T204/T85.

const (
	// DefaultWorkerIdleDebounce avoids double-firing on flappy end_turn edges.
	DefaultWorkerIdleDebounce = 15 * time.Second
	// DefaultDaemonRestartNotifyDelay lets StartAll + wire settle.
	DefaultDaemonRestartNotifyDelay = 12 * time.Second

	eventWorkerIdle       = "worker-idle"
	eventDaemonRestarted  = "daemon-restarted"
	defaultProductPOName  = "jevons-po"
)

// WorkerIdleRef is one work agent mentioned in an idle/restart event body.
type WorkerIdleRef struct {
	Name     string
	Parent   string
	TargetID string
	Purpose  string
	Phase    string
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
	b.WriteString("Your call: send continue / re-brief mission / restart that worker / reap if done / file 🎯 if product gap.\n")
	b.WriteString("Do not wait for the owner to babysit. Local master only (T104) unless Ship was opened.")
	return b.String()
}

// FormatDaemonRestartedText builds one parent-facing restart brief listing
// that parent's running work children (or all known open workers).
func FormatDaemonRestartedText(parent string, workers []WorkerIdleRef) string {
	var b strings.Builder
	b.WriteString("jevonsd restarted (or reattached). Control plane is back.\n\n")
	b.WriteString("Hint: work agents may be process-alive (status=running) but not advancing ")
	b.WriteString("mid-mission — same as resuming a Grok/Claude CLI after Ctrl+C: they often ")
	b.WriteString("need an explicit continue or re-brief, not more waiting.\n\n")
	if parent != "" {
		fmt.Fprintf(&b, "Addressed to: %s\n", parent)
	}
	if len(workers) == 0 {
		b.WriteString("No running work children listed under you right now.\n")
	} else {
		b.WriteString("Running work agents to consider resuming or reaping:\n")
		for _, w := range workers {
			tid := strings.TrimSpace(strings.TrimPrefix(w.TargetID, "🎯"))
			if tid != "" {
				fmt.Fprintf(&b, "  - %s (🎯%s)\n", w.Name, tid)
			} else {
				fmt.Fprintf(&b, "  - %s\n", w.Name)
			}
		}
	}
	b.WriteString("\nDo not blast unrelated agents. Prefer scoped continue/re-brief for open missions; ")
	b.WriteString("reap done workers (T165/T195). Local master only (T104).")
	return b.String()
}

// CollectWorkChildren groups running purpose=work agents by ResolveEventParent.
func CollectWorkChildren(defs []claudia.AgentDef, running func(name string) bool, defaultPO, overseer string) map[string][]WorkerIdleRef {
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
		ref := WorkerIdleRef{
			Name:     d.Name,
			Parent:   parent,
			TargetID: d.TargetID,
			Purpose:  purpose,
			Phase:    "idle",
		}
		out[parent] = append(out[parent], ref)
	}
	return out
}

// HasOpenMissionForIdle is the default open-mission heuristic for enter-idle:
// work purpose with any TargetID, or work purpose always (PO may reap).
// Callers may tighten with MissionOpen(targetID).
func HasOpenMissionForIdle(d claudia.AgentDef, missionOpen func(targetID string) bool) bool {
	purpose := d.Purpose
	if purpose == "" {
		purpose = claudia.PurposeWork
	}
	if purpose != claudia.PurposeWork {
		return false
	}
	tid := strings.TrimSpace(d.TargetID)
	if tid == "" {
		return true // unbound work still visible to PO
	}
	if missionOpen != nil {
		return missionOpen(tid)
	}
	return true
}
