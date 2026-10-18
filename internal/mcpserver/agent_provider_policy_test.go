// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestT691OwnerProviderPolicyReachesClaudiaPlacementAfterReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	planPath := dir
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
	plans, err := claudia.OpenSeatPolicyStore(planPath)
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
	reloadedPlans, err := claudia.OpenSeatPolicyStore(planPath)
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
	policyReload, err := claudia.OpenSeatPolicyStore(planPath)
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
	st := claudia.SeatPolicy{}
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

func TestT691BrokerBusyRaceIsHostDeferralNotSwitchFailure(t *testing.T) {
	sentence := errors.New("switch worker: a turn is in flight; wait for it, or force to interrupt it")
	for _, tc := range []struct {
		name      string
		err       error
		execution string
	}{
		{"quoted sentence", sentence, "failed"},
		{"typed cause", agenterr.WithCause(agenterr.CauseTurnInFlight, sentence), "deferred"},
		{"published sentinel", fmt.Errorf("switch worker: %w", claudia.ErrTurnInFlight), "deferred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			sw := &fakeSwitcher{err: tc.err}
			s.SetModelSwitcher(sw)
			now := time.Now()
			s.SetPlanUsageSource(func() planusage.Snapshot {
				return planusage.Snapshot{At: now, Backends: []planusage.Backend{
					t39015Weekly("claude", 20, 80, now),
					t39015Weekly("codex", 80, 20, now),
				}}
			})
			acts := s.SweepPlanPolicy()
			if len(acts) != 1 || acts[0].Author != claudia.DecisionAuthor || acts[0].Execution != tc.execution {
				t.Fatalf("sweep = %+v, want execution %s", acts, tc.execution)
			}
			if tc.execution == "deferred" && !strings.Contains(acts[0].Failure, "forbids interruption") {
				t.Fatalf("typed busy race was not a host deferral: %+v", acts)
			}
			if tc.execution == "failed" && (acts[0].Failure != tc.err.Error() || acts[0].FailureCause != "") {
				t.Fatalf("quoted sentence became a cause: %+v", acts)
			}
			if switches, _ := sw.calls(); len(switches) != 1 || strings.HasSuffix(switches[0], " force") {
				t.Fatalf("the switch was forced without host consent: %v", switches)
			}
			if got := s.PlanPolicyDecisions(); len(got) != 1 || got[0].Execution != tc.execution {
				t.Fatalf("decision surface = %+v, want execution %s", got, tc.execution)
			}
		})
	}
}

// 🎯T1008: jevons_agent_provider_policy must reach Claudia's own
// Registry.SetSeatPlanPolicy, not only Jevons's seat-policy store, so
// Claudia's own placement sees the same constraints Jevons's sweep does.
func TestT1008ProviderPolicyReachesRegistrySetSeatPlanPolicy(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", SessionID: "session-worker", Provider: claudia.ProviderClaude,
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := claudia.OpenSeatPolicyStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetSeatPlan(plans)
	res, err := s.handleAgentProviderPolicy(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"name": "worker", "actor": s.overseerName(),
			"prefer_provider": "openai-codex", "exclude_providers": []any{"codex"},
			"allow_interrupt": true,
		}},
	})
	if err != nil || res.IsError {
		t.Fatalf("set policy: err=%v res=%v", err, res)
	}
	def := reg.Def("worker")
	if def == nil {
		t.Fatal("worker vanished from registry")
	}
	if def.PreferProvider != claudia.ProviderCodex {
		t.Fatalf("registry PreferProvider = %q, want codex", def.PreferProvider)
	}
	if len(def.ExcludeProviders) != 1 || def.ExcludeProviders[0] != claudia.ProviderCodex {
		t.Fatalf("registry ExcludeProviders = %v, want [codex]", def.ExcludeProviders)
	}
	if !def.HostMayInterrupt {
		t.Fatal("registry HostMayInterrupt was not propagated")
	}
}
