// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

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
// The rule here is the def-side mirror of that strip: whatever the def's
// current provider refuses is dropped, and the drop is named in the log so
// a reader can see what the seat lost.

// providerCapField is one provider-native def setting and the claudia
// capability that governs it.
type providerCapField struct {
	capability claudia.Capability
	requested  func(d *claudia.AgentDef) bool
	clear      func(d *claudia.AgentDef)
}

var providerCapFields = []providerCapField{
	{
		capability: claudia.CapabilitySandboxPolicy,
		requested: func(d *claudia.AgentDef) bool {
			return d.SandboxMode != "" || len(d.SandboxWritableRoots) > 0 || d.SandboxNetworkAccess
		},
		clear: func(d *claudia.AgentDef) {
			d.SandboxMode = ""
			d.SandboxWritableRoots = nil
			d.SandboxNetworkAccess = false
		},
	},
	{
		capability: claudia.CapabilityExtraArgs,
		requested:  func(d *claudia.AgentDef) bool { return len(d.ExtraArgs) > 0 },
		clear:      func(d *claudia.AgentDef) { d.ExtraArgs = nil },
	},
}

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

// DropStaleProviderCaps clears, in place, every def setting its current
// provider refuses, and returns the capabilities it dropped. Call it
// wherever a def's Provider is changed, before the def is registered.
func DropStaleProviderCaps(def *claudia.AgentDef) []claudia.Capability {
	var dropped []claudia.Capability
	for _, f := range providerCapFields {
		if f.requested(def) && claudia.CheckCapability(def.Provider, f.capability) != nil {
			f.clear(def)
			dropped = append(dropped, f.capability)
		}
	}
	return dropped
}

// switchProvider moves def onto target and drops whatever target cannot
// honour, logging the drop so it is never silent.
func switchProvider(def *claudia.AgentDef, target claudia.Provider, why string) {
	from := def.Provider
	def.Provider = target
	if dropped := DropStaleProviderCaps(def); len(dropped) > 0 {
		slog.Warn("provider switch dropped capabilities the new provider refuses",
			"name", def.Name, "from", providerLabel(from), "to", providerLabel(target),
			"dropped", capNames(dropped), "why", why)
	}
}

// ReconcileProviderCaps repairs a registered def that already contradicts
// its provider — minted before this guard, or switched by a path that does
// not go through switchProvider (claudia's recordMigrate). It re-registers
// the scrubbed def and returns what was dropped. Rehydrate calls it before
// Launch, so a seat left in that state is revivable rather than refused
// identically on every retry.
func ReconcileProviderCaps(reg *claudia.Registry, name string) ([]claudia.Capability, error) {
	if reg == nil {
		return nil, nil
	}
	def := reg.Def(name)
	if def == nil {
		return nil, nil
	}
	next := *def
	dropped := DropStaleProviderCaps(&next)
	if len(dropped) == 0 {
		return nil, nil
	}
	if err := reg.Register(next); err != nil {
		return dropped, fmt.Errorf("agent %q: drop %s the %s provider refuses: %w",
			name, capNames(dropped), providerLabel(next.Provider), err)
	}
	slog.Warn("rehydrate dropped capabilities the seat's current provider refuses (stale from a provider switch)",
		"name", name, "provider", providerLabel(next.Provider), "dropped", capNames(dropped))
	return dropped, nil
}

// ExplainLaunchCapError rewrites a launch refusal that names a capability
// the def's provider does not support, so it says the seat changed
// provider and which setting is stale — not a bare client_bug about an
// unsupported capability. Other errors pass through unchanged.
func ExplainLaunchCapError(def *claudia.AgentDef, err error) error {
	var capErr *claudia.CapabilityError
	if err == nil || def == nil || !errors.As(err, &capErr) {
		return err
	}
	return fmt.Errorf("agent %q: stored def carries %s, which the %s provider refuses — "+
		"a setting the seat likely kept across a provider switch; drop it (re-mint or migrate the seat) to revive: %w",
		def.Name, capErr.Capability, providerLabel(def.Provider), err)
}

// rehydrateFailures remembers, per seat, the last launch refusal seen on a
// rehydrate road, so /api/agents can say a stopped seat is known-broken
// rather than serve a plain `stopped` row (🎯T763). Cleared on success.
var rehydrateFailures sync.Map // name -> string

func noteRehydrate(name string, err error) {
	if err == nil {
		rehydrateFailures.Delete(name)
		return
	}
	rehydrateFailures.Store(name, err.Error())
}

// LaunchReconciled is registry.Launch for a seat that may be stopped:
// it first drops settings the def's provider refuses (ReconcileProviderCaps),
// then launches, names a provider-switch cause on a capability refusal, and
// records the outcome for RehydrateHealth.
func LaunchReconciled(reg *claudia.Registry, name string) (*claudia.Agent, error) {
	if reg == nil {
		return nil, fmt.Errorf("launch %q: no agent registry", name)
	}
	if _, err := ReconcileProviderCaps(reg, name); err != nil {
		noteRehydrate(name, err)
		return nil, err
	}
	agent, err := reg.Launch(name)
	err = ExplainLaunchCapError(reg.Def(name), err)
	noteRehydrate(name, err)
	return agent, err
}

// RehydrateHealth classifies whether a stopped seat can be relaunched:
// "broken: <why>" when its last rehydrate failed, "repairable: <why>" when
// its def carries settings its provider refuses (dropped on the next
// rehydrate), else "resumable".
func RehydrateHealth(def claudia.AgentDef) string {
	if v, ok := rehydrateFailures.Load(def.Name); ok {
		return "broken: " + v.(string)
	}
	if stale := StaleProviderCaps(def); len(stale) > 0 {
		return fmt.Sprintf("repairable: stored def carries %s, which the %s provider refuses; dropped on the next rehydrate",
			capNames(stale), providerLabel(def.Provider))
	}
	return "resumable"
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
