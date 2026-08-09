// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package staffops

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ObserveInput is a pure projection of product surfaces for one sentinel tick
// (🎯T219). Built by the harness from registry / cockpit / eventlog / frontier
// — not a second shadow state.
type ObserveInput struct {
	// Overseer usability (T204 surface).
	OverseerAlive      bool
	OverseerAttached   bool // chat stream wired; false when unknown is ok
	OverseerStuckBusy  bool
	OverseerHarnessOK  bool // cockpit already recovered this class
	OverseerGraceDone  bool
	// Fleet agents (phases / dead / deliberate stop / idle residue).
	Agents []AgentObs
	// Eventlog anomaly counts in the recent window.
	Events []EventObs
	// Frontier stall: unattended-ready leaves without engagement (🎯T346).
	// FrontierDepth must be the count of ClassifyLeaf/LeafReady leaves after
	// design/deferred/parked/needs-owner/set_aside-parent filters — not raw
	// graph frontier depth (which still lists gated hubs).
	FrontierDepth   int
	FrontierStalled bool
	FrontierDetail  string
	// Cost alerts already shaped as optional residual signals.
	CostAlerts []CostObs
}

// FrontierStallObs derives ObserveInput frontier stall fields from an
// unattended-ready leaf count (🎯T346). readyCount must already exclude
// design-gated / deferred / parked / needs-owner / set_aside-parent /
// high-infra / parent-with-active-children leaves (ClassifyLeaf LeafReady
// only). Raw graph frontier depth must not be passed here.
//
// Stall fires only when readyCount ≥ 1 and engagedWorkers == 0.
// Capacity-zero deliberate PO sleep (T325.1) is not a frontier stall:
// readyCount 0 (only gated/engaged remain) ⇒ not stalled.
func FrontierStallObs(readyCount, engagedWorkers int) (depth int, stalled bool, detail string) {
	if readyCount < 0 {
		readyCount = 0
	}
	if engagedWorkers < 0 {
		engagedWorkers = 0
	}
	depth = readyCount
	if readyCount > 0 && engagedWorkers == 0 {
		stalled = true
		detail = fmt.Sprintf("unattended-ready leaves=%d engaged_workers=0", readyCount)
	}
	return depth, stalled, detail
}

// FrontierStallObsWithIDs is FrontierStallObs with ready leaf ids in Detail
// (capped) so file+PO missions cite Build leaves, not gated hubs.
func FrontierStallObsWithIDs(readyIDs []string, engagedWorkers int) (depth int, stalled bool, detail string) {
	depth, stalled, detail = FrontierStallObs(len(readyIDs), engagedWorkers)
	if !stalled || len(readyIDs) == 0 {
		return depth, stalled, detail
	}
	const maxIDs = 8
	ids := readyIDs
	suffix := ""
	if len(ids) > maxIDs {
		ids = ids[:maxIDs]
		suffix = fmt.Sprintf(" +%d more", len(readyIDs)-maxIDs)
	}
	detail = fmt.Sprintf("%s ready=[%s%s]", detail, strings.Join(ids, ","), suffix)
	return depth, stalled, detail
}

// AgentObs is one fleet participant observation.
type AgentObs struct {
	Name           string
	Phase          string // idle | working | busy | unknown
	Alive          bool
	DeadHandle     bool // process handle present but dead (T85)
	DeliberateStop bool
	OpenMission    bool
	IdleResidue    bool // idle with open mission after harness settle
	HarnessActed   bool // dead-agent recover / idle nudge already acted
	GraceElapsed   bool
	Detail         string
}

// EventObs is one eventlog-shaped anomaly sample.
type EventObs struct {
	// Kind: busy_storm | restart_thrash | notify_queue | daemon_error | other
	Kind     string
	Symptom  string
	Count    int
	Detail   string
	Severity string // optional
}

// CostObs is a cost monitor alert projection.
type CostObs struct {
	Kind     string
	Detail   string
	Severity string
}

// BuildSignals maps pure observations → Signal list for Classify/RunCycle.
// Mechanical classes (overseer stuck, dead agents, idle residue) set Mechanical
// so policy waits for grace then repair; residual (event storms, frontier stall,
// cost) map to file+PO at medium+ severity.
func BuildSignals(in ObserveInput) []Signal {
	var out []Signal

	// Overseer usability.
	if !in.OverseerAlive {
		out = append(out, Signal{
			Kind:         "overseer_down",
			Symptom:      "overseer:down",
			Severity:     "critical",
			Mechanical:   true, // T204 cockpit owns relaunch
			HarnessActed: in.OverseerHarnessOK,
			GraceElapsed: in.OverseerGraceDone,
			Detail:       "overseer process not alive",
		})
	} else if in.OverseerStuckBusy {
		out = append(out, Signal{
			Kind:         "overseer_stuck",
			Symptom:      "overseer:stuck_busy",
			Severity:     "high",
			Mechanical:   true,
			HarnessActed: in.OverseerHarnessOK,
			GraceElapsed: in.OverseerGraceDone,
			Detail:       "overseer stuck-busy (no ACP progress)",
		})
	}

	// Fleet agents.
	for _, a := range in.Agents {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		if a.DeliberateStop {
			out = append(out, Signal{
				Kind:           "deliberate_stop",
				Symptom:        "stop:" + name,
				Severity:       "low",
				DeliberateStop: true,
				Detail:         firstNonEmpty(a.Detail, "deliberate stop"),
			})
			continue
		}
		if a.DeadHandle {
			out = append(out, Signal{
				Kind:         "dead_agent",
				Symptom:      "dead:" + name,
				Severity:     "high",
				Mechanical:   true, // T85 / cockpit fleet health
				HarnessActed: a.HarnessActed,
				GraceElapsed: a.GraceElapsed,
				Detail:       firstNonEmpty(a.Detail, "dead process handle"),
			})
			continue
		}
		if a.IdleResidue && a.OpenMission {
			out = append(out, Signal{
				Kind:         "fleet_idle_residue",
				Symptom:      "idle:" + name,
				Severity:     "medium",
				Mechanical:   true, // T207 idle nudge owns first response
				HarnessActed: a.HarnessActed,
				GraceElapsed: a.GraceElapsed,
				Detail: firstNonEmpty(a.Detail,
					fmt.Sprintf("open-mission idle residue phase=%s", a.Phase)),
			})
		}
	}

	// Eventlog anomalies.
	for _, e := range in.Events {
		kind := strings.TrimSpace(e.Kind)
		if kind == "" {
			continue
		}
		sym := strings.TrimSpace(e.Symptom)
		if sym == "" {
			sym = kind
		}
		sev := strings.ToLower(strings.TrimSpace(e.Severity))
		if sev == "" {
			sev = "medium"
			if e.Count >= 5 {
				sev = "high"
			}
		}
		// Mechanical-ish storms: busy_storm / notify_queue often have harness
		// paths; treat as residual file+PO when persistent (non-mechanical) so
		// sentinel escalates product gaps after pattern repeats.
		mechanical := false
		switch kind {
		case "notify_queue":
			// Transient not_running may self-heal; residual after grace via
			// non-mechanical file path when count elevated.
			mechanical = e.Count < 3
		}
		out = append(out, Signal{
			Kind:       kind,
			Symptom:    sym,
			Severity:   sev,
			Mechanical: mechanical,
			// For mechanical notify_queue low-count: grace already elapsed if
			// we are sampling residual window (caller sets via EventObs only).
			GraceElapsed: mechanical,
			Detail: firstNonEmpty(e.Detail,
				fmt.Sprintf("count=%d", e.Count)),
		})
	}

	// Frontier stall (🎯T346): only unattended-ready depth, never raw graph depth.
	if in.FrontierStalled && in.FrontierDepth > 0 {
		out = append(out, Signal{
			Kind:       "frontier_stall",
			Symptom:    "stall:frontier",
			Severity:   "high",
			Mechanical: false,
			Detail: firstNonEmpty(in.FrontierDetail,
				fmt.Sprintf("unattended-ready leaves=%d with no engaged work", in.FrontierDepth)),
		})
	}

	// Cost residual.
	for _, c := range in.CostAlerts {
		kind := strings.TrimSpace(c.Kind)
		if kind == "" {
			continue
		}
		sev := strings.ToLower(strings.TrimSpace(c.Severity))
		if sev == "" {
			sev = "medium"
		}
		out = append(out, Signal{
			Kind:       "cost_alert",
			Symptom:    "cost:" + kind,
			Severity:   sev,
			Mechanical: false,
			Detail:     c.Detail,
		})
	}

	return out
}

// EventRow is a minimal eventlog projection for pure anomaly clustering.
type EventRow struct {
	Msg       string
	Component string
	Decision  string
	Level     string
	Source    string
	// Drill is true when the row carries an explicit synthetic marker
	// (eventlog fields.drill truthy). Set by the harness projection.
	Drill bool
	TS    time.Time
}

// drillToken is the normalized source/component stamp used by synthetic RSI
// ops drills (source=rsi-drill, component=rsi_drill).
const drillToken = "rsi_drill"

// drillMsgMarker is the message marker synthetic RSI drills carry.
const drillMsgMarker = "rsi_ops_live_drill"

// drillDecision is the eventlog decision stamped by RSI ops live drills.
const drillDecision = "live_drill"

// normalizeDrillToken lowercases and folds '-' to '_' so rsi-drill and
// rsi_drill compare equal. Exact token match only — a real error from a
// component merely containing "rsi" must not be swallowed.
func normalizeDrillToken(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "_")
}

// IsSyntheticDrillRow reports whether an eventlog row is a synthetic RSI ops
// drill stimulus rather than a real product anomaly (🎯T352). Drill rows are
// appended deliberately to exercise the coach loop (🎯T243/🎯T333); the
// sentinel must ignore them instead of classifying file+PO / daemon_error.
//
// Pure: matches the drill markers only — source or component exactly
// rsi-drill/rsi_drill, msg containing rsi_ops_live_drill, decision live_drill
// from a drill source, or an explicit fields.drill marker.
func IsSyntheticDrillRow(r EventRow) bool {
	if r.Drill {
		return true
	}
	src := normalizeDrillToken(r.Source)
	comp := normalizeDrillToken(r.Component)
	if src == drillToken || comp == drillToken {
		return true
	}
	if strings.Contains(strings.ToLower(r.Msg), drillMsgMarker) {
		return true
	}
	// decision=live_drill alone is not enough — require a drill-ish origin so a
	// real component that ever emits that decision still classifies normally.
	if normalizeDrillToken(r.Decision) == drillDecision &&
		(strings.Contains(src, "drill") || strings.Contains(comp, "drill")) {
		return true
	}
	return false
}

// ClusterEventAnomalies reduces recent eventlog rows into EventObs for BuildSignals.
// Pure: substring/component heuristics only (busy storm, restart thrash, notify_queue).
func ClusterEventAnomalies(rows []EventRow, now time.Time, window time.Duration) []EventObs {
	if window <= 0 {
		window = 15 * time.Minute
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cut := now.Add(-window)

	type agg struct {
		kind    string
		symptom string
		count   int
		sample  string
	}
	by := map[string]*agg{}

	bump := func(key, kind, symptom, sample string) {
		a := by[key]
		if a == nil {
			a = &agg{kind: kind, symptom: symptom}
			by[key] = a
		}
		a.count++
		if a.sample == "" {
			a.sample = sample
		}
	}

	for _, r := range rows {
		if !r.TS.IsZero() && r.TS.Before(cut) {
			continue
		}
		// Synthetic RSI drill stimulus is not a product anomaly (🎯T352).
		if IsSyntheticDrillRow(r) {
			continue
		}
		msg := strings.ToLower(r.Msg + " " + r.Component + " " + r.Decision + " " + r.Level)
		switch {
		case strings.Contains(msg, "notify_queue") || strings.Contains(msg, "not_running"):
			bump("notify_queue", "notify_queue", "event:notify_queue", r.Msg)
		case strings.Contains(msg, "restart") && (strings.Contains(msg, "thrash") ||
			strings.Contains(msg, "bounce") || strings.Contains(msg, "rapid")):
			bump("restart_thrash", "restart_thrash", "event:restart_thrash", r.Msg)
		case strings.Contains(msg, "busy") && (strings.Contains(msg, "storm") ||
			strings.Contains(msg, "stuck") || strings.Contains(msg, "flood")):
			bump("busy_storm", "busy_storm", "event:busy_storm", r.Msg)
		case strings.EqualFold(r.Level, "error") || strings.Contains(msg, "panic"):
			bump("daemon_error:"+r.Component, "daemon_error",
				"event:error:"+firstNonEmpty(r.Component, "daemon"), r.Msg)
		}
	}

	out := make([]EventObs, 0, len(by))
	for _, a := range by {
		if a.count < 1 {
			continue
		}
		// Single daemon_error may be noise; require ≥2 for residual.
		if a.kind == "daemon_error" && a.count < 2 {
			continue
		}
		out = append(out, EventObs{
			Kind:    a.kind,
			Symptom: a.symptom,
			Count:   a.count,
			Detail:  a.sample,
		})
	}
	// Stable order by symptom for hermetic tests.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Symptom < out[j].Symptom
	})
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
