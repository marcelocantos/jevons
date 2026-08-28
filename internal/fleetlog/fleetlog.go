// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fleetlog is the accounted chokepoint for removing an agent from
// the fleet registry (🎯T435).
//
// A removal that reaches agents.json without reaching the event log is a
// registry diff nobody can explain. On 2026-08-10 two 🎯T195 achieve-reaps
// did exactly that — they emitted slog.Info to daily-jevonsd.log and nothing
// to the journal the overseer reads — and two independent competent observers
// each reconstructed a phantom data-loss incident from the gap. The reap was
// correct; the silence was the defect. An unaccounted-for removal costs more
// in misdirected investigation than the reap costs in workers.
//
// So removals do not call claudia's Registry.Remove directly. They go through
// [Account.Remove] or [Account.RemoveSubtree], which name a [Reason] and emit
// an agent_lifecycle/remove event carrying the row that vanished. The rule is
// enforced, not merely documented: TestT435EveryRegistryRemovalIsAccounted
// scans the tree for a registry Remove outside this package and fails on one.
package fleetlog

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// Component and Decision are the stable event coordinates for accounted
// removals — the filter a reader passes to jevons_logs_tail or
// GET /api/logs?component=agent_lifecycle&decision=remove.
const (
	Component = "agent_lifecycle"
	Decision  = "remove"
)

// Reasons an agent leaves the registry. Every removal path in the product
// picks one; the vocabulary is closed so a reader can enumerate the causes of
// a registry diff without reading the code that caused it.
const (
	// ReasonReapDone is 🎯T165/🎯T195 hygiene on a terminal finish report.
	ReasonReapDone = "reap_done"
	// ReasonReapAchieve is 🎯T195 hygiene after the bound target is achieved.
	ReasonReapAchieve = "reap_achieve"
	// ReasonKill is an explicit kill of an agent (and its subtree).
	ReasonKill = "kill"
	// ReasonStopEngagement is the owner's engagement-stop control surface.
	ReasonStopEngagement = "stop_engagement"
	// ReasonAsideDismiss is closing a purpose=aside row (🎯T152).
	ReasonAsideDismiss = "aside_dismiss"
	// ReasonBudgetKill is the 🎯T36 spend kill-switch destroying the fleet.
	ReasonBudgetKill = "budget_kill"
	// ReasonUnbriefedSeat retires a seat whose opening brief never landed
	// (🎯T433) — the seat would otherwise consume the leaf while dead.
	ReasonUnbriefedSeat = "unbriefed_seat"
	// ReasonThreadRemove is a thread/agent dropped by name on request.
	ReasonThreadRemove = "thread_remove"
	// ReasonDeadSeat is the silent-death sweep dropping a work seat whose
	// process exited without a terminal report (🎯T544). A dead work agent is
	// not a parked one: keeping it as a "stopped" row is a zombie in the
	// fleet tree, so the sweep removes it instead of clearing the handle.
	ReasonDeadSeat = "dead_seat"
	// ReasonExpire and ReasonRotationDrop have no caller today. They are part
	// of the vocabulary so a future path that needs them takes a name from
	// this list instead of inventing a silent removal.
	ReasonExpire       = "expire"
	ReasonRotationDrop = "rotation_drop"
	// ReasonUnaccounted is what an empty Reason records as. A caller that
	// reaches the chokepoint without naming a cause is a bug in that caller,
	// and the event says so at warn level rather than reporting a cause it
	// does not know.
	ReasonUnaccounted = "unaccounted"
)

// Reasons returns the closed reason vocabulary, sorted.
func Reasons() []string {
	out := []string{
		ReasonAsideDismiss, ReasonBudgetKill, ReasonExpire, ReasonKill,
		ReasonReapAchieve, ReasonReapDone, ReasonRotationDrop,
		ReasonStopEngagement, ReasonThreadRemove, ReasonUnbriefedSeat,
	}
	sort.Strings(out)
	return out
}

// KnownReason reports whether reason is in the closed vocabulary.
func KnownReason(reason string) bool {
	return slices.Contains(Reasons(), reason)
}

// Logger is the event sink: the shape of server.Server.LogEvent and
// mcpserver.Server.LogEvent, so an Account wires to whichever owns the
// journal without this package importing either.
type Logger func(component, decision string, fields map[string]any)

// Removal is the cause a caller supplies for one removal.
type Removal struct {
	// Reason is one of the Reason constants.
	Reason string
	// Detail is the operator-facing sentence — "reaped on achieve of 🎯T420".
	// It is what turns a row's disappearance into an account of itself.
	Detail string
	// Root names the agent whose removal took this one with it, when this
	// row is a descendant in a subtree removal.
	Root string
	// Actor is who asked (owner, an overseer, a governor). Optional.
	Actor string
	// Fields carries extra event fields. Reserved keys are not overridden.
	Fields map[string]any
}

// Notice is one accounted removal, retained for the fleet surfaces a product
// owner already watches (🎯T435 clause 2 — the reap is legible to the parent
// PO, not only to the log).
type Notice struct {
	At       time.Time
	Name     string
	Reason   string
	Detail   string
	Root     string
	Parent   string
	Purpose  string
	TargetID string
	Err      string
}

// maxNotices bounds the retained ring. Removals are rare; this is a
// safety bound, not a tuning knob.
const maxNotices = 64

// DefaultNoticeWindow is how long a removal stays on the fleet surface. A PO
// that polls the fleet within the window learns why its worker went away.
const DefaultNoticeWindow = 15 * time.Minute

// Account is the accounted-removal chokepoint. The zero value is unusable;
// use New. A nil *Account is safe: removals still happen and still emit to
// slog through the nil Logger path, which is what unwired tests want.
type Account struct {
	mu      sync.Mutex
	log     Logger
	now     func() time.Time
	recent  []Notice
	removed func(name string, rm Removal)
}

// New returns an Account emitting through log (may be nil for slog-only).
func New(log Logger) *Account {
	return &Account{log: log, now: time.Now}
}

// SetClock overrides the clock (tests).
func (a *Account) SetClock(now func() time.Time) {
	if a == nil || now == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.now = now
}

// SetRemovedHook installs a callback run once per row that actually left the
// registry, after the removal and its event.
//
// 🎯T414 wires this to stamp finished-and-reaped intent. The two belong at the
// same chokepoint for the same reason: a removal is a fleet decision, and the
// decision has to survive the row it deleted. A control holding a fleet view
// older than the removal — the notifier of 🎯T413 — otherwise reads the missing
// row as an idle worker somebody should restart, which is what it did.
func (a *Account) SetRemovedHook(fn func(name string, rm Removal)) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.removed = fn
}

// removedHook returns the installed hook (nil-safe).
func (a *Account) removedHook() func(name string, rm Removal) {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.removed
}

// SetLogger rebinds the event sink. Used by daemon wiring that builds the
// Account before the journal-owning server exists.
func (a *Account) SetLogger(log Logger) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log = log
}

// Remove drops name from reg and accounts for it. It reports whether a
// registered row actually left: a name that was not registered is not a
// registry diff, so it is not an event either.
func (a *Account) Remove(reg *claudia.Registry, name string, rm Removal) (bool, error) {
	if a == nil {
		if reg == nil {
			return false, fmt.Errorf("fleetlog: agent registry not available")
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return false, fmt.Errorf("fleetlog: agent name is required")
		}
		if reg.Def(name) == nil {
			return false, nil
		}
		err := reg.Remove(name)
		return err == nil, err
	}
	if reg == nil {
		return false, fmt.Errorf("fleetlog: agent registry not available")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("fleetlog: agent name is required")
	}
	def := reg.Def(name)
	if def == nil {
		return false, nil
	}
	// Snapshot before removal: after Remove the row is gone and the event
	// would have nothing but a name to report.
	n := Notice{
		At:       a.clock(),
		Name:     name,
		Reason:   strings.TrimSpace(rm.Reason),
		Detail:   strings.TrimSpace(rm.Detail),
		Root:     strings.TrimSpace(rm.Root),
		Parent:   def.Parent,
		Purpose:  def.Purpose,
		TargetID: def.TargetID,
	}
	if n.Reason == "" {
		n.Reason = ReasonUnaccounted
	}
	if n.Purpose == "" {
		n.Purpose = claudia.PurposeWork
	}
	err := reg.Remove(name)
	if err != nil {
		n.Err = err.Error()
	}
	a.record(n, rm)
	if err != nil {
		return false, err
	}
	if hook := a.removedHook(); hook != nil {
		hook(name, rm)
	}
	return true, nil
}

// RemoveSubtree removes root's descendants (deepest first) and then root,
// accounting for every row under the same reason. Descendants carry Root so
// a reader can see the one decision that took the whole subtree.
func (a *Account) RemoveSubtree(reg *claudia.Registry, root string, rm Removal) ([]string, error) {
	if reg == nil {
		return nil, fmt.Errorf("fleetlog: agent registry not available")
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("fleetlog: agent name is required")
	}
	var removed []string
	desc := reg.Descendants(root)
	for i := len(desc) - 1; i >= 0; i-- {
		child := rm
		child.Root = root
		if child.Detail == "" {
			child.Detail = fmt.Sprintf("removed with the subtree of %s", root)
		} else {
			child.Detail = fmt.Sprintf("%s (with the subtree of %s)", child.Detail, root)
		}
		ok, err := a.Remove(reg, desc[i], child)
		if ok {
			removed = append(removed, desc[i])
		}
		if err != nil {
			return removed, fmt.Errorf("remove descendant %q of %q: %w", desc[i], root, err)
		}
	}
	ok, err := a.Remove(reg, root, rm)
	if ok {
		removed = append(removed, root)
	}
	if err != nil {
		return removed, fmt.Errorf("remove %q: %w", root, err)
	}
	return removed, nil
}

// Recent returns notices newer than window, oldest first. A zero window uses
// DefaultNoticeWindow.
func (a *Account) Recent(window time.Duration) []Notice {
	if a == nil {
		return nil
	}
	if window <= 0 {
		window = DefaultNoticeWindow
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := a.nowLocked().Add(-window)
	var out []Notice
	for _, n := range a.recent {
		if n.At.After(cutoff) {
			out = append(out, n)
		}
	}
	return out
}

func (a *Account) clock() time.Time {
	if a == nil {
		return time.Now()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nowLocked()
}

func (a *Account) nowLocked() time.Time {
	if a.now == nil {
		return time.Now()
	}
	return a.now()
}

// record appends the notice and emits the event. A nil Account still emits
// (slog-only through eventlog's nil journal path) — silence is the one
// outcome this package must never produce.
func (a *Account) record(n Notice, rm Removal) {
	fields := map[string]any{}
	maps.Copy(fields, rm.Fields)
	fields["name"] = n.Name
	fields["reason"] = n.Reason
	fields["purpose"] = n.Purpose
	if n.Parent != "" {
		fields["parent"] = n.Parent
	}
	if n.TargetID != "" {
		fields["target_id"] = n.TargetID
	}
	if n.Root != "" {
		fields["root"] = n.Root
	}
	if rm.Actor != "" {
		fields["actor"] = rm.Actor
	}
	if n.Detail != "" {
		fields["detail"] = n.Detail
	}
	fields["msg"] = FormatNotice(n)
	switch {
	case n.Err != "":
		fields["outcome"] = "error"
		fields["err"] = n.Err
		fields["level"] = "warn"
	case n.Reason == ReasonUnaccounted:
		// The row left and we cannot say why. That is a caller bug, and a
		// loud one: the whole point of this package is that it cannot happen
		// quietly.
		fields["outcome"] = "ok"
		fields["level"] = "warn"
	default:
		fields["outcome"] = "ok"
	}

	var log Logger
	if a != nil {
		a.mu.Lock()
		log = a.log
		if n.Err == "" {
			a.recent = append(a.recent, n)
			if len(a.recent) > maxNotices {
				a.recent = a.recent[len(a.recent)-maxNotices:]
			}
		}
		a.mu.Unlock()
	}
	if log != nil {
		log(Component, Decision, fields)
	}
}

// FormatNotice renders one removal as the sentence an operator reads.
func FormatNotice(n Notice) string {
	var b strings.Builder
	b.WriteString(n.Name)
	b.WriteString(" left the fleet: ")
	b.WriteString(n.Reason)
	if n.Detail != "" {
		b.WriteString(" — ")
		b.WriteString(n.Detail)
	}
	if n.Err != "" {
		b.WriteString(" [failed: ")
		b.WriteString(n.Err)
		b.WriteString("]")
	}
	return b.String()
}

// FormatNotices renders a block for a fleet surface, or "" for none.
func FormatNotices(ns []Notice) string {
	if len(ns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Recent fleet removals (why rows are missing):\n")
	for _, n := range ns {
		b.WriteString("  ")
		b.WriteString(FormatNotice(n))
		if n.Parent != "" {
			b.WriteString(" (parent ")
			b.WriteString(n.Parent)
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// PrependNotices puts the removal block above a fleet listing so a reader
// who notices a row missing finds the account in the same answer.
func PrependNotices(body string, ns []Notice) string {
	block := FormatNotices(ns)
	if block == "" {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return block
	}
	return block + "\n\n" + body
}
