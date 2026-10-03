// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/claudia"
)

// 🎯T763: a stored AgentDef carries provider-native settings — Codex's
// sandbox (SandboxMode / SandboxWritableRoots / SandboxNetworkAccess) and
// verbatim argv (ExtraArgs). When a seat's provider changes and those
// settings ride along, the def contradicts itself: claudia refuses a
// sandbox on claude, and extra_args on codex, at every launch. Rehydrate
// is deterministic, so the seat can never come back, and /api/agents
// shows an ordinary `stopped` row (jevons-po, 2026-09-21 07:45).
//
// claudia's own live Migrate strips the same fields from the process
// config (migrateDestConfig) but its recordMigrate leaves them on the def.
// The rule here is the def-side mirror of that strip, with one limit: a
// setting is dropped only when dropping it cannot widen what the seat may
// do beyond what the fleet grants a seat minted fresh on the new provider.
// A setting that is a restriction the new provider cannot enforce is
// refused instead, loudly, and the seat keeps its old def. Every drop is
// logged and served on /api/agents (dropped_caps), never silent.

// providerCapField is one provider-native def setting and the claudia
// capability that governs it.
type providerCapField struct {
	capability claudia.Capability
	requested  func(d *claudia.AgentDef) bool
	// keep says why the setting must NOT be dropped when the provider
	// refuses it (a restriction that would be lost); "" means droppable.
	keep  func(d *claudia.AgentDef) string
	clear func(d *claudia.AgentDef)
	// show describes the setting's value for the drop record.
	show func(d *claudia.AgentDef) string
}

var providerCapFields = []providerCapField{
	{
		capability: claudia.CapabilitySandboxPolicy,
		requested: func(d *claudia.AgentDef) bool {
			return d.SandboxMode != "" || len(d.SandboxWritableRoots) > 0 || d.SandboxNetworkAccess ||
				d.SandboxGitWrite
		},
		// The fleet mints codex work seats "workspace-write" and every
		// other provider with no sandbox (CodexWorkSandbox), so dropping
		// workspace-write — or the looser danger-full-access, or roots and
		// network that only widen a sandbox — leaves the seat where a fresh
		// mint on the new provider would be. "read-only", and any mode
		// claudia does not name, is a restriction nothing on the new
		// provider would enforce: dropping it would widen the seat.
		keep: func(d *claudia.AgentDef) string {
			switch d.SandboxMode {
			case "", "workspace-write", "danger-full-access":
				return ""
			}
			return fmt.Sprintf("sandbox %q is a restriction the new provider cannot enforce; dropping it would widen the seat's access", d.SandboxMode)
		},
		clear: func(d *claudia.AgentDef) {
			d.SandboxMode = ""
			d.SandboxWritableRoots = nil
			d.SandboxNetworkAccess = false
			// 🎯T849: the git grant is a widening of workspace-write, so it
			// drops with the mode. Left behind it is worse than useless —
			// claudia counts it as a requested sandbox policy, so a claude
			// seat carrying it is refused at every launch, which is the
			// deterministic-rehydrate death this strip exists to prevent.
			d.SandboxGitWrite = false
		},
		show: func(d *claudia.AgentDef) string {
			return fmt.Sprintf("mode=%q roots=%d network=%v git_write=%v",
				d.SandboxMode, len(d.SandboxWritableRoots), d.SandboxNetworkAccess, d.SandboxGitWrite)
		},
	},
	{
		capability: claudia.CapabilityExtraArgs,
		requested:  func(d *claudia.AgentDef) bool { return len(d.ExtraArgs) > 0 },
		// Jevons never sets ExtraArgs; an operator did, and argv can carry
		// a restriction (a permission mode, a disallowed tool). Nothing
		// here can tell which, so it is never dropped behind anyone's back.
		keep: func(d *claudia.AgentDef) string {
			return fmt.Sprintf("extra_args %q were set by hand and may carry a restriction; they are not dropped automatically", d.ExtraArgs)
		},
		clear: func(d *claudia.AgentDef) { d.ExtraArgs = nil },
		show:  func(d *claudia.AgentDef) string { return fmt.Sprintf("%q", d.ExtraArgs) },
	},
}

// ErrProviderSwitchWouldWeaken is wrapped by a refusal to drop a setting
// that is a restriction the def's provider cannot enforce.
var ErrProviderSwitchWouldWeaken = errors.New("provider switch would drop a restriction")

// StaleProviderCaps names the capabilities def requests that its current
// provider refuses. Empty means the def is launchable as far as
// provider-native settings go.
func StaleProviderCaps(def claudia.AgentDef) []claudia.Capability {
	var stale []claudia.Capability
	for _, f := range providerCapFields {
		if f.requested(&def) && claudia.CheckCapability(def.Provider, f.capability) != nil {
			stale = append(stale, f.capability)
		}
	}
	return stale
}

// dropStaleProviderCaps clears, in place, every def setting its current
// provider refuses, and describes each drop. When any such setting must be
// kept (a restriction that would be lost), def is left untouched and the
// error wraps ErrProviderSwitchWouldWeaken.
func dropStaleProviderCaps(def *claudia.AgentDef) ([]string, error) {
	var drops []providerCapField
	for _, f := range providerCapFields {
		if !f.requested(def) || claudia.CheckCapability(def.Provider, f.capability) == nil {
			continue
		}
		if why := f.keep(def); why != "" {
			return nil, fmt.Errorf("agent %q on %s: %s: %w",
				def.Name, providerLabel(def.Provider), why, ErrProviderSwitchWouldWeaken)
		}
		drops = append(drops, f)
	}
	var dropped []string
	for _, f := range drops {
		dropped = append(dropped, string(f.capability)+" "+f.show(def))
		f.clear(def)
	}
	return dropped, nil
}

// providerSwitchRefusal reports whether moving def onto target would have
// to drop a restriction. Switch paths check it before stopping anything, so
// a refused switch leaves the seat exactly as it was.
func providerSwitchRefusal(def claudia.AgentDef, target claudia.Provider) error {
	def.Provider = target
	_, err := dropStaleProviderCaps(&def)
	return err
}

// switchProvider moves def onto target and drops whatever target cannot
// honour, recording the drop so it is never silent. It refuses, leaving
// def untouched, when a drop would widen the seat.
func switchProvider(def *claudia.AgentDef, target claudia.Provider, why string) error {
	next := *def
	from := next.Provider
	next.Provider = target
	dropped, err := dropStaleProviderCaps(&next)
	if err != nil {
		return err
	}
	*def = next
	if len(dropped) > 0 {
		noteCapDrop(def.Name, fmt.Sprintf("%s→%s (%s): dropped %s",
			providerLabel(from), providerLabel(target), why, strings.Join(dropped, "; ")))
	}
	return nil
}

// ReconcileProviderCaps repairs a registered def that already contradicts
// its provider — minted before this guard, or switched by a path that does
// not go through switchProvider (claudia's recordMigrate). It re-registers
// the scrubbed def and records the drop. Rehydrate calls it before Launch,
// so such a seat is revivable rather than refused identically on every
// retry — unless the stale setting is a restriction, which is refused here
// with an error naming it.
func ReconcileProviderCaps(reg *claudia.Registry, name string) error {
	if reg == nil {
		return nil
	}
	def := reg.Def(name)
	if def == nil {
		return nil
	}
	next := *def
	dropped, err := dropStaleProviderCaps(&next)
	if err != nil || len(dropped) == 0 {
		return err
	}
	if err := reg.Register(next); err != nil {
		return fmt.Errorf("agent %q: drop %s the %s provider refuses: %w",
			name, strings.Join(dropped, "; "), providerLabel(next.Provider), err)
	}
	noteCapDrop(name, fmt.Sprintf("rehydrate on %s: dropped stale %s",
		providerLabel(next.Provider), strings.Join(dropped, "; ")))
	return nil
}

// ExplainLaunchCapError rewrites a launch refusal over a capability. When
// the def carries that capability's provider-native setting, it names the
// setting and the provider that refuses it. Any other capability refusal
// is labelled as such — not blamed on a provider switch it has no evidence
// of. Non-capability errors pass through unchanged.
func ExplainLaunchCapError(def *claudia.AgentDef, err error) error {
	var capErr *claudia.CapabilityError
	if err == nil || def == nil || !errors.As(err, &capErr) {
		return err
	}
	for _, f := range providerCapFields {
		if f.capability == capErr.Capability && f.requested(def) {
			return fmt.Errorf("agent %q: stored def carries %s (%s), which the %s provider refuses — "+
				"a provider-native setting kept across a provider switch; re-mint or migrate the seat to revive: %w",
				def.Name, capErr.Capability, f.show(def), providerLabel(def.Provider), err)
		}
	}
	return fmt.Errorf("agent %q: the %s provider refused capability %s at launch "+
		"(not a stored provider-switch setting): %w",
		def.Name, providerLabel(def.Provider), capErr.Capability, err)
}

// rehydrateFailures remembers, per seat, the last launch refusal seen on a
// rehydrate road, so /api/agents can say a stopped seat is known-broken
// rather than serve a plain `stopped` row (🎯T763). Cleared on success.
var rehydrateFailures sync.Map // name -> string

// capDrops remembers, per seat, the last provider-native setting dropped
// from its def, served on /api/agents so a drop is visible where the seat
// is, not only in the daemon log.
var capDrops sync.Map // name -> string

func noteRehydrate(name string, err error) {
	if err == nil {
		rehydrateFailures.Delete(name)
	} else {
		rehydrateFailures.Store(name, err.Error())
		logPlanAuthLoss(name, err)
	}
	saveRehydrateFailures()
}

// logPlanAuthLoss writes the one line that makes a plan login loss
// attributable from jevonsd.log alone (🎯T947): when it happened, this
// process's pid (there is no cross-process way to name which broker or
// sidecar consumed the refresh token), and the seat whose rehydrate
// surfaced it. Every other consumer only reads the shared token; if one of
// them is why it rotated, that fact lives in claudia's own log, not this
// one — this line is jevons's half of "the logs alone" name the cause.
func logPlanAuthLoss(name string, err error) {
	if err == nil || !PlanAuthFailure(err.Error()) {
		return
	}
	slog.Warn("plan auth: login failure recorded for seat",
		"seat", name, "pid", os.Getpid(), "time", time.Now().UTC().Format(time.RFC3339), "err", err)
}

// RecordRehydrateFailure updates the fleet row after a recovery operation
// that fails before LaunchReconciled can run (for example, plan reauth).
func RecordRehydrateFailure(name string, err error) { noteRehydrate(name, err) }

func noteCapDrop(name, what string) {
	capDrops.Store(name, time.Now().UTC().Format(time.RFC3339)+" "+what)
	slog.Warn("provider-native settings dropped from agent def", "name", name, "detail", what)
}

// CapDrop returns the last recorded settings drop for name, or "".
func CapDrop(name string) string {
	if v, ok := capDrops.Load(name); ok {
		return v.(string)
	}
	return ""
}

// LaunchReconciled is registry.Launch for a seat that may be stopped:
// it first drops settings the def's provider refuses (ReconcileProviderCaps),
// then launches, explains a capability refusal, and records the outcome for
// RehydrateHealth.
func LaunchReconciled(reg *claudia.Registry, name string) (*claudia.Agent, error) {
	defer seatstate.ObserveRegistrySeat(reg, name)
	if reg == nil {
		return nil, fmt.Errorf("launch %q: no agent registry", name)
	}
	if err := ReconcileProviderCaps(reg, name); err != nil {
		noteRehydrate(name, err)
		return nil, err
	}
	agent, err := LaunchRecording(reg, name)
	err = ExplainLaunchCapError(reg.Def(name), err)
	noteRehydrate(name, err)
	return agent, err
}

// RehydrateHealth classifies whether a stopped seat can be relaunched:
// "broken: <why>" when its last rehydrate failed or its def carries a
// restriction its provider cannot enforce, "repairable: <why>" when its
// def carries settings its provider refuses that the next rehydrate drops,
// else "resumable".
func RehydrateHealth(def claudia.AgentDef) string {
	if v, ok := rehydrateFailures.Load(def.Name); ok {
		return "broken: " + v.(string)
	}
	stale := StaleProviderCaps(def)
	if len(stale) == 0 {
		return "resumable"
	}
	probe := def
	if _, err := dropStaleProviderCaps(&probe); err != nil {
		return "broken: " + err.Error()
	}
	return fmt.Sprintf("repairable: stored def carries %s, which the %s provider refuses; dropped on the next rehydrate",
		capNames(stale), providerLabel(def.Provider))
}

func providerLabel(p claudia.Provider) string {
	if p == "" {
		return string(claudia.ProviderClaude)
	}
	return string(p)
}

func capNames(caps []claudia.Capability) string {
	names := make([]string, len(caps))
	for i, c := range caps {
		names[i] = string(c)
	}
	return strings.Join(names, ",")
}
