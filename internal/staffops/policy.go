// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package staffops implements one bounded staff ops cycle: health-of-health
// classification plus a compact resource snapshot for the root overseer
// (🎯T325.4). Pure policy aligns with the T219 sentinel action set
// (harness-ok | repair | file+PO | ignore) without a permanent monologue.
package staffops

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Action is the health-of-health classification for one signal or cycle.
type Action string

const (
	// ActionHarnessOK: mechanical floor handled or system healthy.
	ActionHarnessOK Action = "harness-ok"
	// ActionRepair: bounded control-plane repair (rehydrate, interrupt, nudge).
	ActionRepair Action = "repair"
	// ActionFilePO: residual product gap — file target and brief jevons-po.
	ActionFilePO Action = "file+PO"
	// ActionIgnore: false alarm, grace wait, or cooldown suppress.
	ActionIgnore Action = "ignore"
)

// EventSource stamps root-directed staff ops deliveries.
const EventSource = "staff-ops"

// DefaultCooldown is how long the same symptom is suppressed after file+PO.
const DefaultCooldown = time.Hour

// Signal is one observed health-of-health sample (cockpit/fleet/event).
type Signal struct {
	// Kind is a coarse class: dead_agent, busy_storm, notify_queue,
	// restart_thrash, frontier_stall, cost_alert, harness_recovered, …
	Kind string
	// Symptom is the cooldown fingerprint (stable id for "same symptom").
	Symptom string
	// Severity: low | medium | high | critical (empty = medium).
	Severity string
	// Mechanical is true when T204/T207/T85 harness already owns the class.
	Mechanical bool
	// HarnessActed is true when harness already recovered or acted.
	HarnessActed bool
	// GraceElapsed is true after the documented grace bound for mechanical.
	GraceElapsed bool
	// Detail is optional free text for the wire report (not used in policy).
	Detail string
}

// Decision is the pure classification of one signal.
type Decision struct {
	Signal Signal
	Action Action
	Reason string
}

// ResourceSnapshot is the compact resource brief delivered to root.
type ResourceSnapshot struct {
	SessionCount     int
	GlobalUSDPerHour float64
	FleetUSDPerHour  float64
	SpentTodayUSD    float64
	Accounting       string // list_price | subscription | …
	FrontierDepth    int
	IdlePOCount      int
	RunningAgents    int
	StoppedAgents    int
	// Note is optional (e.g. "cost monitor off").
	Note string
}

// Cooldown tracks last file+PO time per symptom (in-memory for thin vertical).
type Cooldown struct {
	// LastFile maps symptom fingerprint → last file+PO time (UTC).
	LastFile map[string]time.Time
	// Duration defaults to DefaultCooldown when zero.
	Duration time.Duration
}

// ShouldSuppress reports whether file+PO for symptom is still in cooldown.
func (c *Cooldown) ShouldSuppress(symptom string, now time.Time) bool {
	if c == nil || symptom == "" || c.LastFile == nil {
		return false
	}
	last, ok := c.LastFile[symptom]
	if !ok {
		return false
	}
	d := c.Duration
	if d <= 0 {
		d = DefaultCooldown
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.Sub(last) < d
}

// MarkFiled records a file+PO for symptom at now (mutates receiver).
func (c *Cooldown) MarkFiled(symptom string, now time.Time) {
	if c == nil || symptom == "" {
		return
	}
	if c.LastFile == nil {
		c.LastFile = make(map[string]time.Time)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.LastFile[symptom] = now.UTC()
}

// CycleArgs configures one pure staff ops cycle.
type CycleArgs struct {
	Signals   []Signal
	Resources ResourceSnapshot
	// Cooldown is optional; when non-nil and a file+PO is chosen, MarkFiled runs
	// unless DryRun.
	Cooldown *Cooldown
	// Now defaults to time.Now().UTC().
	Now time.Time
	// DryRun builds wire text and does not update cooldown.
	DryRun bool
}

// CycleResult is the outcome of one bounded ops cycle.
type CycleResult struct {
	// Primary is the highest-priority action among decisions (or harness-ok).
	Primary   Action
	Decisions []Decision
	Resources ResourceSnapshot
	// FiledSymptoms lists symptoms classified file+PO this cycle (after cooldown).
	FiledSymptoms []string
	// WireText is the compact message for root (not owner monologue).
	WireText string
}

// Classify maps one signal to harness-ok | repair | file+PO | ignore.
// Cooldown is applied by the caller (or RunCycle) before accepting file+PO.
func Classify(sig Signal) Decision {
	sev := strings.ToLower(strings.TrimSpace(sig.Severity))
	if sev == "" {
		sev = "medium"
	}
	kind := strings.TrimSpace(sig.Kind)
	if kind == "" && strings.TrimSpace(sig.Symptom) == "" {
		return Decision{Signal: sig, Action: ActionIgnore, Reason: "empty signal"}
	}

	// Healthy / recovered mechanical path.
	if sig.HarnessActed {
		return Decision{
			Signal: sig,
			Action: ActionHarnessOK,
			Reason: "harness already acted",
		}
	}

	// Mechanical floor: wait for grace; after grace, repair not thrash-file.
	if sig.Mechanical {
		if !sig.GraceElapsed {
			return Decision{
				Signal: sig,
				Action: ActionIgnore,
				Reason: "mechanical class within grace bound",
			}
		}
		return Decision{
			Signal: sig,
			Action: ActionRepair,
			Reason: "mechanical residual after grace — bounded repair",
		}
	}

	// Non-mechanical residual: severity gates file vs ignore.
	switch sev {
	case "low":
		return Decision{
			Signal: sig,
			Action: ActionIgnore,
			Reason: "low severity residual — ignore",
		}
	case "medium", "high", "critical":
		return Decision{
			Signal: sig,
			Action: ActionFilePO,
			Reason: "non-mechanical residual — file+PO",
		}
	default:
		return Decision{
			Signal: sig,
			Action: ActionIgnore,
			Reason: "unknown severity — ignore",
		}
	}
}

// actionRank: higher means more urgent for Primary selection.
func actionRank(a Action) int {
	switch a {
	case ActionFilePO:
		return 4
	case ActionRepair:
		return 3
	case ActionHarnessOK:
		return 2
	case ActionIgnore:
		return 1
	default:
		return 0
	}
}

// RunCycle classifies all signals, applies file+PO cooldown, builds primary
// action and a compact wire report for the root overseer.
func RunCycle(args CycleArgs) CycleResult {
	now := args.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	out := CycleResult{
		Primary:   ActionHarnessOK,
		Resources: args.Resources,
	}

	if len(args.Signals) == 0 {
		out.Decisions = nil
		out.WireText = FormatReport(out)
		return out
	}

	decisions := make([]Decision, 0, len(args.Signals))
	var filed []string
	for i, sig := range args.Signals {
		d := Classify(sig)
		if d.Action == ActionFilePO {
			sym := strings.TrimSpace(sig.Symptom)
			if sym == "" {
				sym = strings.TrimSpace(sig.Kind)
			}
			if args.Cooldown != nil && args.Cooldown.ShouldSuppress(sym, now) {
				d.Action = ActionIgnore
				d.Reason = "cooldown: same symptom re-file suppressed"
			} else {
				filed = append(filed, sym)
				if !args.DryRun && args.Cooldown != nil {
					args.Cooldown.MarkFiled(sym, now)
				}
			}
		}
		decisions = append(decisions, d)
		// Primary is the max among decisions only (empty cycle stays harness-ok).
		if i == 0 || actionRank(d.Action) > actionRank(out.Primary) {
			out.Primary = d.Action
		}
	}
	out.Decisions = decisions
	out.FiledSymptoms = filed
	out.WireText = FormatReport(out)
	return out
}

// FormatReport builds the compact root-bound brief (staff ops, not monologue).
func FormatReport(res CycleResult) string {
	var b strings.Builder
	b.WriteString("[staff-ops] one cycle (🎯T325.4) — bounded, not permanent monologue\n")
	fmt.Fprintf(&b, "Primary: %s\n", res.Primary)
	b.WriteString(FormatResourceSnapshot(res.Resources))
	if len(res.Decisions) == 0 {
		b.WriteString("Health-of-health: no signals — harness-ok\n")
	} else {
		b.WriteString("Health-of-health:\n")
		// Stable order by symptom/kind for hermetic tests.
		sorted := append([]Decision(nil), res.Decisions...)
		sort.SliceStable(sorted, func(i, j int) bool {
			si, sj := sorted[i].Signal, sorted[j].Signal
			ka := si.Symptom
			if ka == "" {
				ka = si.Kind
			}
			kb := sj.Symptom
			if kb == "" {
				kb = sj.Kind
			}
			return ka < kb
		})
		for _, d := range sorted {
			sym := d.Signal.Symptom
			if sym == "" {
				sym = d.Signal.Kind
			}
			fmt.Fprintf(&b, "  - %s → %s (%s)", sym, d.Action, d.Reason)
			if d.Signal.Detail != "" {
				fmt.Fprintf(&b, " detail=%s", d.Signal.Detail)
			}
			b.WriteByte('\n')
		}
	}
	if len(res.FiledSymptoms) > 0 {
		b.WriteString("File+PO candidates: ")
		b.WriteString(strings.Join(res.FiledSymptoms, ", "))
		b.WriteByte('\n')
		b.WriteString("Root: decide file/brief-PO/repair/ignore. Staff cycle does not implement product code or open Ship.\n")
	} else if res.Primary == ActionRepair {
		b.WriteString("Root: consider bounded repair (rehydrate/interrupt/nudge). No product implement by staff.\n")
	} else {
		b.WriteString("Root: snapshot only — no file/repair action required this cycle.\n")
	}
	return b.String()
}

// FormatResourceSnapshot is the resource section of the wire brief.
func FormatResourceSnapshot(r ResourceSnapshot) string {
	var b strings.Builder
	b.WriteString("Resource snapshot:\n")
	fmt.Fprintf(&b, "  sessions=%d running_agents=%d stopped_agents=%d idle_po=%d frontier_depth=%d\n",
		r.SessionCount, r.RunningAgents, r.StoppedAgents, r.IdlePOCount, r.FrontierDepth)
	acct := r.Accounting
	if acct == "" {
		acct = "unknown"
	}
	fmt.Fprintf(&b, "  burn global=$%.2f/hr fleet=$%.2f/hr spent_today=$%.2f accounting=%s\n",
		r.GlobalUSDPerHour, r.FleetUSDPerHour, r.SpentTodayUSD, acct)
	if r.Note != "" {
		fmt.Fprintf(&b, "  note: %s\n", r.Note)
	}
	return b.String()
}
