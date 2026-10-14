// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/server"
)

// costGuard is the wired-up token-spend clamp-down (🎯T36) plus standing
// cost-safety auditor posture (🎯T334): collector → monitor → enforcer on
// one usage.db spine, with eventlog audit trail and overseer-visible alerts
// on trip. Overseer is always in ProtectedWorkers (never budget-killed).
type costGuard struct {
	monitor  *cost.Monitor
	enforcer *cost.Enforcer
	// store is the usage spine, shared with capacity admission (🎯T359) for
	// today's token total — the honest lever under subscription accounting.
	store *cost.Store
	// config reports the live budget policy (session bound, daily lines).
	config func() *cost.BudgetConfig
}

// startCostGuard constructs and starts the clamp-down loops. It returns
// nil (with a logged warning) rather than failing the daemon if the
// usage DB can't open — cost monitoring is important but must not be a
// single point of failure for the whole cockpit.
func startCostGuard(ctx context.Context, jc config.Config, watcher *config.Watcher, registry *claudia.Registry, _ *discovery.Scanner, srv *server.Server) *costGuard {
	budgetPath := filepath.Join(jc.StateDir, "budget.json")
	// 🎯T574: budget.json is hot — limits, protected workers, and the
	// accounting mode swap in ≤5 s. The configured overseer is always
	// protected, whatever the file says: killing the CEO's own brain is
	// never an acceptable enforcement outcome. `disabled` decides whether
	// the collector/enforcer exist at all, so flipping it forces a bounce.
	// bootDisabled is seeded from the first load AFTER Watch returns.
	// OnChange does not fire for that first load (🎯T574: first load
	// seeds the baseline); a later flip of `disabled` still bounces.
	var bootDisabled bool
	budget, err := config.Watch(watcher, &config.WatchArgs[*cost.BudgetConfig]{
		Path:     budgetPath,
		Load:     budgetLoader(jc.OverseerName),
		Fallback: cost.DefaultBudgetConfig(),
		OnChange: func(c *cost.BudgetConfig) {
			if c != nil && c.Disabled != bootDisabled {
				bounceForConfig(srv, budgetPath, []string{"disabled"})
			}
		},
	})
	if err != nil {
		slog.Error("cost: bad budget.json — using defaults", "err", err, "path", budgetPath)
	}
	cfg := budget.Get()
	if cfg != nil {
		bootDisabled = cfg.Disabled
	}
	// Owner opt-out via budget.json (🎯T137): no collector/enforcer, but
	// /api/cost still reports disabled so the UI hides $ honestly.
	if cfg.Disabled {
		slog.Info("cost: clamp-down disabled", "path", budgetPath, "reason", "budget.json disabled=true",
			"hint", "set disabled=false and accounting=subscription for SuperGrok (API-eq $ never enforces)")
		srv.SetCostSource(func() any {
			return map[string]any{
				"disabled":      true,
				"accounting":    cost.AccountingDisabled,
				"billable":      false,
				"currency_note": "cost monitoring disabled",
			}
		})
		return nil
	}
	if cfg.IsSubscription() {
		slog.Info("cost: subscription accounting — USD estimates never pause/kill",
			"path", budgetPath, "accounting", cost.AccountingSubscription)
	}

	store, err := cost.OpenStore(filepath.Join(jc.StateDir, "usage.db"))
	if err != nil {
		slog.Error("cost: usage db unavailable — clamp-down disabled", "err", err)
		return nil
	}
	config := budget.Get

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

	collector := cost.NewCollector(&cost.CollectorArgs{
		Store:        store,
		ProjectsRoot: jc.SessionsDir,
		Attribute:    attribute,
	})

	monitor := cost.NewMonitor(&cost.MonitorArgs{
		Store:           store,
		Config:          config,
		CollectorHealth: collector.Health,
	})

	// Notify: UI cost_alert + overseer-visible alert (🎯T334). Structured
	// audit trail is dual-written via AuditTrail → eventlog cost_clamp.
	notify := func(level cost.Level, msg string) {
		slog.Warn("budget clamp-down", "level", level, "msg", msg)
		srv.Broadcast(map[string]any{"type": "cost_alert", "level": level.String(), "message": msg})
		// Overseer channel: fire-and-forget so clamp path never blocks on LLM.
		// Rate-limited by enforcer (only on level transitions / acts).
		if level >= cost.LevelWarn {
			body := butler.FormatEventPush(cost.EventSource, cost.FormatOverseerAlert(level, msg))
			if err := srv.DeliverToOverseerAs(body, "agent"); err != nil {
				slog.Warn("cost-safety: overseer alert deliver failed", "err", err, "level", level)
			}
		}
	}
	auditTrail := func(d cost.ClampDecision) {
		fields := cost.AuditFields(d)
		fields["decision"] = d.Decision
		// Durable journal: component=cost_clamp for jevons_logs_tail /api/logs.
		srv.LogEvent(cost.AuditComponent, d.Decision, fields)
	}

	enforcer := cost.NewEnforcer(&cost.EnforcerArgs{
		Snapshot: monitor.Snapshot,
		Config:   config,
		Actions: &fleetActions{
			registry: registry,
			killswitch: &cost.BrokerKillSwitch{
				List:       brokerSeatNames,
				Registered: func(name string) bool { return registry.Def(name) != nil },
				Stop:       claudia.StopBrokerSeat,
				Protect:    append([]string{}, cfg.ProtectedWorkers...),
			},
			// 🎯T139 / T334: never stop or kill the overseer on budget clamp.
			protect:  append([]string{}, cfg.ProtectedWorkers...),
			removals: srv.RemovalAccount(),
		},
		Notify:      notify,
		OwnerNotice: srv.NotifyOwnerNote,
		AuditTrail:  auditTrail,
	})

	go collector.Run(ctx, cost.DefaultScanInterval, cost.DefaultPollInterval)
	go enforcer.Run(ctx, 0)
	slog.Info("cost clamp-down started", "budget", budgetPath,
		"cost_safety", "T334", "protected", cfg.ProtectedWorkers)

	return &costGuard{monitor: monitor, enforcer: enforcer, store: store, config: config}
}

// brokerSeatNames lists the names of every seat the claudia broker holds.
func brokerSeatNames(ctx context.Context) ([]string, error) {
	seats, err := claudia.BrokerSeats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seats))
	for _, s := range seats {
		out = append(out, s.Name)
	}
	return out, nil
}

// fleetActions maps enforcement decisions onto the claudia registry and
// the broker kill-switch.
type fleetActions struct {
	registry   *claudia.Registry
	killswitch *cost.BrokerKillSwitch
	// protect: agent names never stopped by fleet pause / PauseWorker (🎯T139).
	// KillWorker still may remove non-overseer workers; protect blocks Stop.
	protect []string
	// removals accounts for each budget kill in the event log (🎯T435).
	// A clamp-down that empties the fleet is the largest registry diff this
	// product can produce, and the one most likely to be read as a collapse.
	removals *fleetlog.Account
}

func (a *fleetActions) isProtected(id string) bool {
	for _, p := range a.protect {
		if p == id {
			return true
		}
	}
	return false
}

func (a *fleetActions) PauseWorker(id string) error {
	if a.isProtected(id) {
		slog.Info("budget: skip pause of protected worker", "name", id)
		return nil
	}
	a.registry.Stop(id) // resumable — the thread and session persist
	return nil
}

func (a *fleetActions) KillWorker(id string) error {
	if a.isProtected(id) {
		// Never deregister the overseer; pause path already no-ops protect.
		slog.Info("budget: skip kill of protected worker", "name", id)
		return nil
	}
	a.registry.Stop(id)
	_, err := a.removals.Remove(a.registry, id, fleetlog.Removal{
		Reason: fleetlog.ReasonBudgetKill,
		Detail: "destroyed by the budget clamp-down kill-switch (🎯T36)",
		Actor:  "cost-enforcer",
	})
	return err
}

func (a *fleetActions) StopFleet() error {
	// 🎯T139: StopAll would grey the overseer and undeliver chat. Stop
	// everyone else; leave protected names (overseer) running.
	protect := map[string]bool{}
	for _, p := range a.protect {
		protect[p] = true
	}
	for _, d := range a.registry.List() {
		if protect[d.Name] {
			continue
		}
		a.registry.Stop(d.Name)
	}
	return nil
}

func (a *fleetActions) KillSwitch() error {
	// Stop the broker seats no connection owns first (orphans the
	// registry has lost), then stop what the registry still tracks —
	// still skipping protected overseer (🎯T139).
	err := a.killswitch.Kill()
	_ = a.StopFleet()
	return err
}

// budgetLoader reads budget.json with the overseer pinned into
// protected_workers, whatever the file says.
func budgetLoader(overseer string) func(path string) (*cost.BudgetConfig, error) {
	return func(path string) (*cost.BudgetConfig, error) {
		c, err := cost.LoadBudgetConfig(path)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(c.ProtectedWorkers, overseer) {
			c.ProtectedWorkers = append(c.ProtectedWorkers, overseer)
		}
		return c, nil
	}
}
