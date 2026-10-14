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
	"github.com/marcelocantos/jevons/internal/planusage"
)

func switchReq(name, provider string) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": name, "provider": provider}
	return req
}

// 🎯T790: a switch slower than the caller's deadline still lands, and a
// retry reads the recorded outcome instead of starting a second switch.
func TestT790SwitchSurvivesCallerDeadline(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.toolDeadline = 30 * time.Millisecond
	release := make(chan struct{})
	var runs atomic.Int32
	var ctxCancelled atomic.Bool
	done := make(chan struct{})
	h := s.boundTool("jevons_agent_switch_model", s.detachSwitch(func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		runs.Add(1)
		<-release
		ctxCancelled.Store(ctx.Err() != nil)
		defer close(done)
		return mcp.NewToolResultText("po switched grok → claude in place"), nil
	}))

	res, err := h(context.Background(), switchReq("po", "claude"))
	if err != nil || !strings.Contains(toolText(res), "T790") {
		t.Fatalf("caller must be told the move continues (T790): %q err=%v", toolText(res), err)
	}
	// A retry while in flight joins it.
	res, _ = h(context.Background(), switchReq("po", "claude"))
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
		res, _ = h(context.Background(), switchReq("po", "claude"))
		if strings.Contains(toolText(res), "switched grok") {
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

// A fast switch answers directly and leaves no stale outcome behind.
func TestT790FastSwitchLeavesNoStaleOutcome(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	var runs atomic.Int32
	h := s.boundTool("jevons_agent_switch_model", s.detachSwitch(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		runs.Add(1)
		return mcp.NewToolResultText("ok"), nil
	}))
	for i := 1; i <= 2; i++ {
		if _, err := h(context.Background(), switchReq("po", "claude")); err != nil {
			t.Fatal(err)
		}
		if int(runs.Load()) != i {
			t.Fatalf("call %d reused a stale outcome (runs=%d)", i, runs.Load())
		}
	}
}

func TestPlanSweepPassesClaudiaModelToTheSwitch(t *testing.T) {
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
	fake := &fakeSwitcher{reg: reg, live: true}
	s.SetModelSwitcher(fake)
	acts := s.SweepPlanPolicy()
	switches, _ := fake.calls()
	if len(acts) != 1 || acts[0].Model == "" || len(switches) != 1 || !strings.HasSuffix(switches[0], "/"+acts[0].Model) {
		t.Fatalf("Claudia's model was not passed to the switch: actions=%+v switches=%v", acts, switches)
	}
	if acts[0].Execution != "switched" {
		t.Fatalf("execution = %q, want switched", acts[0].Execution)
	}
}

// The tool passes the model through and reports how the seat switched.
func TestSwitchToolHonoursModelAndReportsHow(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	fake := &fakeSwitcher{live: true}
	s.SetModelSwitcher(fake)
	req := switchReq("po", "claude")
	req.Params.Arguments.(map[string]any)["model"] = "claude-opus-5"
	req.Params.Arguments.(map[string]any)["owner_asked"] = true
	res, err := s.handleAgentSwitchModel(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	switches, _ := fake.calls()
	if len(switches) != 1 || switches[0] != "po→claude/claude-opus-5" {
		t.Fatalf("switch = %v, want po→claude/claude-opus-5", switches)
	}
	if !strings.Contains(toolText(res), "in place") {
		t.Fatalf("result does not say the seat switched in place: %q", toolText(res))
	}

	fake.live = false
	res, err = s.handleAgentSwitchModel(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolText(res), "while stopped") {
		t.Fatalf("result does not say the stopped seat resumes on the new model: %q", toolText(res))
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
	res, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if out := toolText(res); !strings.Contains(out, "provider=claude") || !strings.Contains(out, "model=claude-opus-5") {
		t.Fatalf("agent_list lacks provider/model: %q", out)
	}
}
