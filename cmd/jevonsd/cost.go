// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/server"
)

// costGuard is the wired-up token-spend clamp-down (🎯T36): a collector
// tailing every active session JSONL, a monitor computing burn-rates and
// runaway signals, and an enforcer that escalates to the fleet and the
// launchd-detached-reaching kill-switch.
type costGuard struct {
	monitor  *cost.Monitor
	enforcer *cost.Enforcer
}

// startCostGuard constructs and starts the clamp-down loops. It returns
// nil (with a logged warning) rather than failing the daemon if the
// usage DB can't open — cost monitoring is important but must not be a
// single point of failure for the whole cockpit.
func startCostGuard(ctx context.Context, jc config.Config, registry *claudia.Registry, _ *discovery.Scanner, srv *server.Server) *costGuard {
	budgetPath := filepath.Join(jc.StateDir, "budget.json")
	cfg, err := cost.LoadBudgetConfig(budgetPath)
	if err != nil {
		slog.Error("cost: bad budget.json — using defaults", "err", err, "path", budgetPath)
		cfg = cost.DefaultBudgetConfig()
	}
	// Owner opt-out: SuperGrok / no marginal $ — clamp + dollar UI off (🎯T137 revisit).
	if cfg.Disabled {
		slog.Info("cost: clamp-down and live $ reporting disabled", "path", budgetPath, "reason", "budget.json disabled=true")
		return nil
	}

	store, err := cost.OpenStore(filepath.Join(jc.StateDir, "usage.db"))
	if err != nil {
		slog.Error("cost: usage db unavailable — clamp-down disabled", "err", err)
		return nil
	}
	// The configured overseer must always be protected — killing the CEO's
	// own brain is never an acceptable enforcement outcome.
	protected := false
	for _, w := range cfg.ProtectedWorkers {
		if w == jc.OverseerName {
			protected = true
		}
	}
	if !protected {
		cfg.ProtectedWorkers = append(cfg.ProtectedWorkers, jc.OverseerName)
	}
	config := func() *cost.BudgetConfig { return cfg }

	// Attribution: a session id maps to a jevons worker when the registry
	// owns it; otherwise it is unattributed (foreign, or a lost orphan).
	attribute := func(sessionID string) string {
		for _, d := range registry.List() {
			if d.SessionID == sessionID {
				return d.Name
			}
		}
		return ""
	}

	sock := cost.ClaudiaSocketPath()

	collector := cost.NewCollector(&cost.CollectorArgs{
		Store:        store,
		ProjectsRoot: jc.SessionsDir,
		Attribute:    attribute,
	})

	// Orphan = a burning session that lives INSIDE the jevons fleet tmux
	// server but is not owned by the registry (a lost worker whose
	// cockpit is gone — the incident's signature). A merely unattributed
	// burner NOT in the fleet is the owner's own terminal: foreign spend,
	// reported via the global vs fleet split, never flagged as an orphan.
	isOrphan := func(r cost.BurnRow) bool {
		if r.Worker != "" {
			return false
		}
		return fleetSessionSet(sock)[r.SessionID]
	}

	monitor := cost.NewMonitor(&cost.MonitorArgs{
		Store:             store,
		Config:            config,
		IsOrphan:          isOrphan,
		CollectorLastPoll: collector.LastPoll,
	})

	notify := func(level cost.Level, msg string) {
		slog.Warn("budget clamp-down", "level", level, "msg", msg)
		srv.Broadcast(map[string]any{"type": "cost_alert", "level": level.String(), "message": msg})
	}

	enforcer := cost.NewEnforcer(&cost.EnforcerArgs{
		Snapshot: monitor.Snapshot,
		Config:   config,
		Actions:  &fleetActions{registry: registry, killswitch: &cost.TmuxKillSwitch{Socket: sock}},
		Notify:   notify,
	})

	go collector.Run(ctx, cost.DefaultScanInterval, cost.DefaultPollInterval)
	go enforcer.Run(ctx, 0)
	slog.Info("cost clamp-down started", "budget", budgetPath, "fleet_socket", sock)

	return &costGuard{monitor: monitor, enforcer: enforcer}
}

// fleetSessionSet returns the set of Grok session ids currently hosted
// as windows in the fleet tmux server. Best-effort: if the server is
// down or the option is absent, it returns empty, so orphan detection
// degrades to a safe no-op rather than mis-flagging foreign sessions.
func fleetSessionSet(sock string) map[string]bool {
	out, err := exec.Command("tmux", "-S", sock, "list-windows", "-a", "-F", "#{@claudia-session-id}").Output()
	set := map[string]bool{}
	if err != nil {
		return set
	}
	for _, line := range strings.Fields(string(out)) {
		if discovery.IsUUID(line) {
			set[line] = true
		}
	}
	return set
}

// fleetActions maps enforcement decisions onto the claudia registry and
// the tmux kill-switch.
type fleetActions struct {
	registry   *claudia.Registry
	killswitch *cost.TmuxKillSwitch
}

func (a *fleetActions) PauseWorker(id string) error {
	a.registry.Stop(id) // resumable — the thread and session persist
	return nil
}

func (a *fleetActions) KillWorker(id string) error {
	a.registry.Stop(id)
	return a.registry.Remove(id)
}

func (a *fleetActions) StopFleet() error {
	a.registry.StopAll()
	return nil
}

func (a *fleetActions) KillSwitch() error {
	// Reap the launchd-detached fleet server first (reaches orphans the
	// registry has lost), then stop what the registry still tracks.
	err := a.killswitch.Kill()
	a.registry.StopAll()
	return err
}
