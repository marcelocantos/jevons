// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package staffops implements staff / sentinel observe→classify→act policy
// (🎯T325.4 thin cycle + 🎯T219 durable sentinel). Pure classification maps
// signal→harness-ok | repair | file+PO | ignore with grace, cooldown, and
// max-actions/hour false-alarm control. No product code implement; no Ship.
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
	// ActionIgnore: false alarm, grace wait, cooldown, rate limit, deliberate stop.
	ActionIgnore Action = "ignore"
)

// EventSource stamps root-directed staff ops / sentinel deliveries.
const EventSource = "staff-ops"

// SentinelEventSource stamps continuous sentinel audit/act deliveries.
const SentinelEventSource = "sentinel"

// DefaultCooldown is how long the same symptom is suppressed after file+PO.
const DefaultCooldown = time.Hour

// DefaultMaxActionsPerHour caps repair+file actions in a sliding hour (T219).
const DefaultMaxActionsPerHour = 12

// DefaultMechanicalGrace is the documented grace before escalating mechanical
// classes already owned by T204/T207/T85.
const DefaultMechanicalGrace = 2 * time.Minute

// Signal is one observed health-of-health sample (cockpit/fleet/event).
type Signal struct {
	// Kind is a coarse class: dead_agent, busy_storm, notify_queue,
	// restart_thrash, frontier_stall, fleet_blocked, cost_alert, overseer_down,
	// overseer_stuck, fleet_idle_residue, harness_recovered, deliberate_stop, …
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
	// DeliberateStop is true when the agent was intentionally stopped (T90/T165
	// spirit) — never thrash-interrupt.
	DeliberateStop bool
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

// Cooldown tracks last file+PO time per symptom (in-memory or durable).
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

// ActionBudget is a sliding-window cap on repair/file+PO acts (T219 false-alarm).
type ActionBudget struct {
	// MaxPerHour defaults to DefaultMaxActionsPerHour when ≤0.
	MaxPerHour int
	// History is timestamps of counted actions (UTC), pruned on use.
	History []time.Time
}

// max returns the effective hourly cap.
func (b *ActionBudget) max() int {
	if b == nil || b.MaxPerHour <= 0 {
		return DefaultMaxActionsPerHour
	}
	return b.MaxPerHour
}

// prune drops history older than one hour before now.
func (b *ActionBudget) prune(now time.Time) {
	if b == nil || len(b.History) == 0 {
		return
	}
	cut := now.Add(-time.Hour)
	kept := b.History[:0]
	for _, t := range b.History {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	b.History = kept
}

// Count returns how many actions count in the last hour.
func (b *ActionBudget) Count(now time.Time) int {
	if b == nil {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	b.prune(now)
	return len(b.History)
}

// Allow reports whether another repair/file action is within budget.
func (b *ActionBudget) Allow(now time.Time) bool {
	if b == nil {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return b.Count(now) < b.max()
}

// Record appends an action at now (mutates). No-op when DryRun path skips.
func (b *ActionBudget) Record(now time.Time) {
	if b == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	b.prune(now)
	b.History = append(b.History, now.UTC())
}

// CycleArgs configures one pure staff ops / sentinel cycle.
type CycleArgs struct {
	Signals   []Signal
	Resources ResourceSnapshot
	// Cooldown is optional; when non-nil and a file+PO is chosen, MarkFiled runs
	// unless DryRun.
	Cooldown *Cooldown
	// Budget is optional max repair/file+PO per sliding hour.
	Budget *ActionBudget
	// Now defaults to time.Now().UTC().
	Now time.Time
	// DryRun builds wire text and does not update cooldown or budget.
	DryRun bool
	// Sentinel marks wire text for the continuous loop (🎯T219) vs one-shot T325.4.
	Sentinel bool
}

// CycleResult is the outcome of one bounded ops cycle.
type CycleResult struct {
	// Primary is the highest-priority action among decisions (or harness-ok).
	Primary   Action
	Decisions []Decision
	Resources ResourceSnapshot
	// FiledSymptoms lists symptoms classified file+PO this cycle (after cooldown).
	FiledSymptoms []string
	// RepairSymptoms lists symptoms classified repair this cycle (after budget).
	RepairSymptoms []string
	// WireText is the compact message for root / PO (not owner monologue).
	WireText string
	// ActionsCounted is how many budget slots this cycle consumed.
	ActionsCounted int
}

// Classify maps one signal to harness-ok | repair | file+PO | ignore.
// Cooldown and rate budget are applied by the caller (or RunCycle).
func Classify(sig Signal) Decision {
	if sig.DeliberateStop {
		return Decision{
			Signal: sig,
			Action: ActionIgnore,
			Reason: "deliberate stop — do not thrash",
		}
	}

	sev := strings.ToLower(strings.TrimSpace(sig.Severity))
	if sev == "" {
		sev = "medium"
	}
	kind := strings.TrimSpace(sig.Kind)
	if kind == "" && strings.TrimSpace(sig.Symptom) == "" {
		return Decision{Signal: sig, Action: ActionIgnore, Reason: "empty signal"}
	}

	// 🎯T407: a blocked fleet is a report, never a spawn mission. This
	// wins before residual file+PO so a high-severity fleet_blocked
	// signal cannot be read as "unattended ready leaves".
	if kind == "fleet_blocked" {
		cause := strings.TrimPrefix(strings.TrimSpace(sig.Symptom), "blocked:")
		if cause == "" {
			cause = firstNonEmpty(sig.Detail, "blocked")
		}
		return Decision{
			Signal: sig,
			Action: ActionHarnessOK,
			Reason: "fleet cannot run — " + cause + "; do not spawn",
		}
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

// countsAsAction reports whether the action consumes the hourly budget.
func countsAsAction(a Action) bool {
	return a == ActionRepair || a == ActionFilePO
}

// RunCycle classifies all signals, applies file+PO cooldown and action budget,
// builds primary action and a compact wire report.
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
		out.WireText = FormatReport(out, args.Sentinel)
		return out
	}

	decisions := make([]Decision, 0, len(args.Signals))
	var filed, repaired []string
	actionsCounted := 0

	for i, sig := range args.Signals {
		d := Classify(sig)
		sym := strings.TrimSpace(sig.Symptom)
		if sym == "" {
			sym = strings.TrimSpace(sig.Kind)
		}

		if d.Action == ActionFilePO {
			if args.Cooldown != nil && args.Cooldown.ShouldSuppress(sym, now) {
				d.Action = ActionIgnore
				d.Reason = "cooldown: same symptom re-file suppressed"
			}
		}

		// Max actions/hour: demote repair and file+PO after budget exhausted.
		if countsAsAction(d.Action) {
			if args.Budget != nil && !args.Budget.Allow(now) {
				d.Action = ActionIgnore
				d.Reason = "max actions/hour — rate limited"
			} else {
				if d.Action == ActionFilePO {
					filed = append(filed, sym)
					if !args.DryRun && args.Cooldown != nil {
						args.Cooldown.MarkFiled(sym, now)
					}
				} else if d.Action == ActionRepair {
					repaired = append(repaired, sym)
				}
				if !args.DryRun && args.Budget != nil {
					args.Budget.Record(now)
				}
				actionsCounted++
			}
		}

		decisions = append(decisions, d)
		if i == 0 || actionRank(d.Action) > actionRank(out.Primary) {
			out.Primary = d.Action
		}
	}
	out.Decisions = decisions
	out.FiledSymptoms = filed
	out.RepairSymptoms = repaired
	out.ActionsCounted = actionsCounted
	out.WireText = FormatReport(out, args.Sentinel)
	return out
}

// FormatReport builds the compact root/PO-bound brief (staff, not monologue).
// sentinel=true stamps 🎯T219 continuous-loop copy.
func FormatReport(res CycleResult, sentinel bool) string {
	var b strings.Builder
	if sentinel {
		b.WriteString("[sentinel] continuous observe→classify→act (🎯T219) — pure watcher; control-plane repair only; no product implement; no Ship\n")
	} else {
		b.WriteString("[staff-ops] one cycle (🎯T325.4) — bounded, not permanent monologue\n")
	}
	fmt.Fprintf(&b, "Primary: %s\n", res.Primary)
	b.WriteString(FormatResourceSnapshot(res.Resources))
	if len(res.Decisions) == 0 {
		b.WriteString("Health-of-health: no signals — harness-ok\n")
	} else {
		b.WriteString("Health-of-health:\n")
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
		if sentinel {
			b.WriteString("Act: deliver mission to jevons-po (spawn-only) + file/brief residual; sentinel does not implement product code or open Ship.\n")
		} else {
			b.WriteString("Root: decide file/brief-PO/repair/ignore. Staff cycle does not implement product code or open Ship.\n")
		}
	} else if res.Primary == ActionRepair {
		if sentinel {
			b.WriteString("Act: bounded control-plane repair (rehydrate/interrupt/nudge). Verify harness first; no product implement.\n")
		} else {
			b.WriteString("Root: consider bounded repair (rehydrate/interrupt/nudge). No product implement by staff.\n")
		}
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

// FormatPOMission builds the jevons-po mission text for a file+PO decision.
// PO is spawn-only (T125); sentinel never implements product code.
func FormatPOMission(res CycleResult) string {
	var b strings.Builder
	b.WriteString("[sentinel mission 🎯T219] Residual wrongness — file bullseye if missing, then spawn Build workers under parent=jevons-po (T125 spawn-only; no product implement by PO).\n")
	fmt.Fprintf(&b, "Primary: %s\n", res.Primary)
	if len(res.FiledSymptoms) > 0 {
		b.WriteString("Symptoms: ")
		b.WriteString(strings.Join(res.FiledSymptoms, ", "))
		b.WriteByte('\n')
	}
	for _, d := range res.Decisions {
		if d.Action != ActionFilePO {
			continue
		}
		sym := d.Signal.Symptom
		if sym == "" {
			sym = d.Signal.Kind
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", sym, d.Signal.Kind, d.Reason)
		if d.Signal.Detail != "" {
			fmt.Fprintf(&b, "  evidence: %s\n", d.Signal.Detail)
		}
	}
	b.WriteString("Constraints: no Ship plane; local Build only unless owner opens Ship. Cite oracle evidence on finish.\n")
	return b.String()
}
