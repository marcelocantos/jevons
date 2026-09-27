// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/thread"
)

func migrateReq(name, provider string) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": name, "provider": provider}
	return req
}

// 🎯T790: a migrate slower than the caller's deadline still lands, and a
// retry reads the recorded outcome instead of starting a second move.
func TestT790MigrateSurvivesCallerDeadline(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.toolDeadline = 30 * time.Millisecond
	release := make(chan struct{})
	var runs atomic.Int32
	var ctxCancelled atomic.Bool
	done := make(chan struct{})
	h := s.boundTool("jevons_agent_migrate", s.detachMigrate(func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		runs.Add(1)
		<-release
		ctxCancelled.Store(ctx.Err() != nil)
		defer close(done)
		return mcp.NewToolResultText("po migrated grok → claude"), nil
	}))

	res, err := h(context.Background(), migrateReq("po", "claude"))
	if err != nil || !strings.Contains(toolText(res), "T790") {
		t.Fatalf("caller must be told the move continues (T790): %q err=%v", toolText(res), err)
	}
	// A retry while in flight joins it.
	res, _ = h(context.Background(), migrateReq("po", "claude"))
	if !strings.Contains(toolText(res), "T790") || runs.Load() != 1 {
		t.Fatalf("retry must join the in-flight move; runs=%d text=%q", runs.Load(), toolText(res))
	}
	close(release)
	<-done
	if ctxCancelled.Load() {
		t.Fatal("move context was cancelled by the caller deadline")
	}
	// Once finished, the retry reads the outcome; still one run.
	deadline := time.Now().Add(2 * time.Second)
	for {
		res, _ = h(context.Background(), migrateReq("po", "claude"))
		if strings.Contains(toolText(res), "migrated grok") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("outcome never readable: %q", toolText(res))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runs.Load() != 1 {
		t.Fatalf("retry started a second move: runs=%d", runs.Load())
	}
}

// A fast migrate answers directly and leaves no stale outcome behind.
func TestT790FastMigrateLeavesNoStaleOutcome(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	var runs atomic.Int32
	h := s.boundTool("jevons_agent_migrate", s.detachMigrate(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		runs.Add(1)
		return mcp.NewToolResultText("ok"), nil
	}))
	for i := 1; i <= 2; i++ {
		if _, err := h(context.Background(), migrateReq("po", "claude")); err != nil {
			t.Fatal(err)
		}
		if int(runs.Load()) != i {
			t.Fatalf("call %d reused a stale outcome (runs=%d)", i, runs.Load())
		}
	}
}

type pinFake struct {
	model string
	p     handover.Pending
}

func (f *pinFake) PrepareMigration(string, claudia.Provider, bool) (handover.Pending, error) {
	return f.p, nil
}
func (f *pinFake) PrepareMigrationPinned(_ string, _ claudia.Provider, m string, _ bool) (handover.Pending, error) {
	f.model = m
	return f.p, nil
}
func (f *pinFake) CompleteThinBrief(p handover.Pending) (handover.Pending, error) { return p, nil }
func (f *pinFake) SeedSuccessor(string) (handover.Pending, bool, error)           { return f.p, false, nil }
func (f *pinFake) Launch(*thread.Thread) error                                    { return nil }

func TestPlanSweepPassesClaudiaModelToMigrator(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "worker", SessionID: "old", Provider: "xai-oauth", Purpose: claudia.PurposeWork}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t39015Weekly("grok", 10, 90, now),
			t39015Weekly("claude", 90, 10, now),
		}}
	})
	fake := &pinFake{p: handover.Pending{Agent: "worker", From: "grok", To: "claude", Remap: handover.RemapClaudiaMigrate}}
	s.SetMigrator(fake)
	acts := s.SweepPlanPolicy()
	if len(acts) != 1 || acts[0].Model == "" || fake.model != acts[0].Model {
		t.Fatalf("Claudia model was not passed to migration: actions=%+v received=%q", acts, fake.model)
	}
}

// 🎯T790: the model parameter reaches the migrator, and an unread session id
// is surfaced to the caller.
func TestT790MigrateHonoursModelAndReportsUnreadSession(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	fake := &pinFake{p: handover.Pending{Agent: "po", From: "grok", To: "claude",
		Remap: handover.RemapClaudiaMigrate, SessionUnread: true}}
	s.SetMigrator(fake)
	req := migrateReq("po", "claude")
	req.Params.Arguments.(map[string]any)["model"] = "claude-opus-5"
	res, err := s.handleAgentMigrate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if fake.model != "claude-opus-5" {
		t.Fatalf("model not passed: %q", fake.model)
	}
	if !strings.Contains(toolText(res), "not Materialized") {
		t.Fatalf("unread session not surfaced: %q", toolText(res))
	}
}

// agent_list shows provider and model per row.
func TestT790AgentListShowsProviderAndModel(t *testing.T) {
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "po", WorkDir: "/w", SessionID: "s1",
		Provider: claudia.ProviderClaude, Model: "claude-opus-5", Purpose: claudia.PurposeWork}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.registry = reg
	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out := toolText(res); !strings.Contains(out, "provider=claude") || !strings.Contains(out, "model=claude-opus-5") {
		t.Fatalf("agent_list lacks provider/model: %q", out)
	}
}
