// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatplan"
	"github.com/mark3labs/mcp-go/mcp"
)

type busyPlanMigrator struct {
	sweepLedger
	force bool
}

type pendingClaudiaMigrator struct {
	sweepLedger
	registry *claudia.Registry
	plans    *seatplan.Store
	attempts int
	fail     bool
}

type failedAfterPersistMigrator struct {
	sweepLedger
	registry *claudia.Registry
	plans    *seatplan.Store
}

func (m *failedAfterPersistMigrator) PrepareMigration(name string, to claudia.Provider, _ bool) (handover.Pending, error) {
	def := *m.registry.Def(name)
	from, session := def.Provider, def.SessionID
	def.Provider = cli.SubscriptionSeatProvider(to)
	def.SessionID = "persisted-destination"
	if err := m.registry.Register(def); err != nil {
		return handover.Pending{}, err
	}
	if err := m.plans.Put(name, seatplan.State{
		MigrationFrom: from, MigrationFromSession: session,
		MigrationSeed: "bounded handover awaiting delivery", MigrationPendingStart: true,
	}); err != nil {
		return handover.Pending{}, err
	}
	return handover.Pending{Agent: name, To: string(to)}, fmt.Errorf("destination launch unavailable")
}

func (m *pendingClaudiaMigrator) PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error) {
	m.attempts++
	if force || to != claudia.ProviderCodex {
		return handover.Pending{}, fmt.Errorf("unexpected pending retry: provider=%s force=%t", to, force)
	}
	if m.fail {
		return handover.Pending{}, fmt.Errorf("destination launch unavailable")
	}
	if err := m.plans.Update(name, func(st *seatplan.State) {
		st.MigrationSeed = ""
		st.MigrationPendingStart = false
	}); err != nil {
		return handover.Pending{}, err
	}
	return handover.Pending{Agent: name, To: string(to), Remap: handover.RemapClaudiaMigrate, Delivered: true}, nil
}

func (m *busyPlanMigrator) PrepareMigration(_ string, _ claudia.Provider, force bool) (handover.Pending, error) {
	m.force = force
	return handover.Pending{}, fmt.Errorf("Migrate: turn in flight; wait for the current response or Interrupt first")
}

func TestT691OwnerProviderPolicyReachesClaudiaPlacementAfterReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	planPath := filepath.Join(dir, "seatplan.json")
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", SessionID: "session-worker", Provider: claudia.ProviderClaude,
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := seatplan.Open(planPath)
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetSeatPlan(plans)
	call := func(args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := s.handleAgentProviderPolicy(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{Arguments: args},
		})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := call(map[string]any{"name": "worker", "actor": "other", "exclude_providers": []any{"codex"}}); !res.IsError {
		t.Fatal("non-overseer changed provider policy")
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(),
		"prefer_provider": "openai-codex", "exclude_providers": []any{"codex"}}); res.IsError {
		t.Fatalf("set policy: %s", toolText(res))
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(),
		"allowed_providers": []any{}}); res.IsError || !strings.Contains(toolText(res), "allowed=none") {
		t.Fatalf("explicit allow-none: %s", toolText(res))
	}
	reloaded, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	reloadedPlans, err := seatplan.Open(planPath)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reloaded)
	s.SetSeatPlan(reloadedPlans)
	if res := call(map[string]any{"name": "worker"}); res.IsError ||
		!strings.Contains(toolText(res), "allowed=none") || !strings.Contains(toolText(res), "prefer=codex") {
		t.Fatalf("reloaded policy: %s", toolText(res))
	}
	now := time.Now()
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 20, 80, now),
			t39015Weekly("codex", 80, 20, now),
			t39015Weekly("grok", 55, 45, now),
		}}
	})
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].Action != planusage.SeatPark || got[0].Author != claudia.DecisionAuthor {
		t.Fatalf("allow-none should park through Claudia: %+v", got)
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(), "allow_park": false}); res.IsError {
		t.Fatalf("forbid host park: %s", toolText(res))
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(), "allow_interrupt": true}); res.IsError {
		t.Fatalf("opt in to host interruption: %s", toolText(res))
	}
	policyReload, err := seatplan.Open(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if st := policyReload.Get("worker"); !st.HostMayInterrupt || !st.HostNeverPark {
		t.Fatalf("host constraints were not durable: %+v", st)
	}
	if got := s.SweepPlanPolicy(); len(got) != 1 || got[0].Action != planusage.SeatPark ||
		got[0].Execution != "deferred" || !strings.Contains(got[0].Failure, "forbids parking") {
		t.Fatalf("Jevons should defer Claudia's park verdict: %+v", got)
	}
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].Execution != "deferred" {
		t.Fatalf("read-only view hid host deferral: %+v", got)
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(), "allow_any": true}); res.IsError {
		t.Fatalf("allow-any: %s", toolText(res))
	}
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].To != "grok" {
		t.Fatalf("explicit Codex ban should pick Grok: %+v", got)
	}
	if res := call(map[string]any{"name": "worker", "actor": s.overseerName(), "exclude_providers": []any{}}); res.IsError {
		t.Fatalf("clear exclusions: %s", toolText(res))
	}
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].To != "codex" {
		t.Fatalf("preferred Codex should be eligible again: %+v", got)
	}
}

func TestT691HostInterruptPolicyDefersBusySeatUnlessOptedIn(t *testing.T) {
	action := planusage.PlanAction{Action: planusage.SeatMigrate}
	st := seatplan.State{}
	if got := hostPlanDeferral(action, st, true); !strings.Contains(got, "forbids interruption") {
		t.Fatalf("default in-flight migration was not deferred: %q", got)
	}
	st.HostMayInterrupt = true
	if got := hostPlanDeferral(action, st, true); got != "" {
		t.Fatalf("opted-in interrupt still deferred: %q", got)
	}
	st.HostNeverPark = true
	if got := hostPlanDeferral(planusage.PlanAction{Action: planusage.SeatPark}, st, false); !strings.Contains(got, "forbids parking") {
		t.Fatalf("host park ban was lost: %q", got)
	}
}

func TestT691BrokerBusyRaceIsHostDeferralNotMigrationFailure(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", SessionID: "source", Provider: claudia.ProviderClaude,
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	migrator := &busyPlanMigrator{}
	s.SetMigrator(migrator)
	now := time.Now()
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 20, 80, now),
			t39015Weekly("codex", 80, 20, now),
		}}
	})
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].Author != claudia.DecisionAuthor ||
		acts[0].Execution != "deferred" || !strings.Contains(acts[0].Failure, "forbids interruption") ||
		migrator.force {
		t.Fatalf("broker busy race was not a host deferral: acts=%+v force=%t", acts, migrator.force)
	}
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].Execution != "deferred" {
		t.Fatalf("decision surface lost busy deferral: %+v", got)
	}
}

func TestT691PendingClaudiaHandoverRetriesWithoutHotSourceOrPlanFeed(t *testing.T) {
	// Journey exception for the failed-launch branch: J34 proves a live
	// broker-backed move and restart. This test injects the precise durable
	// state left between Claudia's registry write and provider launch; Claudia's
	// TestStoppedMigrationPersistsOneTransferAcrossFailedLaunch proves that
	// retry keeps the destination ID and does not repeat the paid transfer.
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", SessionID: "destination-session", Provider: claudia.ProviderCodex,
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := seatplan.Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := plans.Put("worker", seatplan.State{
		MigrationFrom: claudia.ProviderGrok, MigrationFromSession: "source-session",
		MigrationSeed: "pending bounded handover", MigrationPendingStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetSeatPlan(plans)
	migrator := &pendingClaudiaMigrator{registry: reg, plans: plans, fail: true}
	s.SetMigrator(migrator)
	if acts := s.SweepPlanPolicy(); len(acts) != 1 || acts[0].Execution != "pending" ||
		!strings.Contains(acts[0].Failure, "destination launch unavailable") {
		t.Fatalf("failed Claudia handover disappeared without a plan feed: %+v", acts)
	}
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].Author != claudia.DecisionAuthor ||
		got[0].Execution != "pending" || !strings.Contains(got[0].Failure, "destination launch unavailable") {
		t.Fatalf("owner decision surface hid Claudia's pending failure: %+v", got)
	}
	now := time.Now()
	hot := true
	s.SetPlanUsageSource(func() planusage.Snapshot {
		remaining, used := 80.0, 20.0
		if hot {
			remaining, used = 20, 80
		}
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("codex", remaining, used, now),
			t39015Weekly("grok", 80, 20, now),
		}}
	})
	migrator.fail = false
	if acts := s.SweepPlanPolicy(); len(acts) != 1 || acts[0].Execution != "migrated" || acts[0].Failure != "" {
		t.Fatalf("retry did not finish Claudia's persisted handover: %+v", acts)
	}
	if migrator.attempts != 2 || plans.Get("worker").MigrationSeed != "" {
		t.Fatalf("retry did not settle the same destination: attempts=%d state=%+v", migrator.attempts, plans.Get("worker"))
	}
	hot = false
	if acts := s.SweepPlanPolicy(); len(acts) != 0 || migrator.attempts != 2 {
		t.Fatalf("completed handover was retried: acts=%+v attempts=%d", acts, migrator.attempts)
	}
}

func TestT691FirstFailedLaunchReportsClaudiaPendingInsteadOfFailed(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", SessionID: "source", Provider: claudia.ProviderClaude,
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := seatplan.Open("")
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetSeatPlan(plans)
	s.SetMigrator(&failedAfterPersistMigrator{registry: reg, plans: plans})
	now := time.Now()
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("claude", 20, 80, now),
			t39015Weekly("codex", 80, 20, now),
		}}
	})
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].Author != claudia.DecisionAuthor || acts[0].Execution != "pending" ||
		!strings.Contains(acts[0].Failure, "destination launch unavailable") {
		t.Fatalf("persisted Claudia destination reported as a failed move: %+v", acts)
	}
	if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].Execution != "pending" ||
		got[0].Author != claudia.DecisionAuthor || !strings.Contains(got[0].Failure, "destination launch unavailable") {
		t.Fatalf("decision surface hid the pending launch: %+v", got)
	}
}
