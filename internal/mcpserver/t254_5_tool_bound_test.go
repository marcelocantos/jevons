// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestT254_5_1BoundToolDeadline(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.toolDeadline = 40 * time.Millisecond
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	h := s.boundTool("jevons_agent_start", func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		<-block
		return mcp.NewToolResultText("ok"), nil
	})
	start := time.Now()
	res, err := h(t.Context(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("hung tools/call must return an MCP error")
	}
	if !strings.Contains(toolText(res), "T254.5.1") {
		t.Fatalf("error %q should cite T254.5.1", toolText(res))
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Fatalf("deadline waited %s", time.Since(start))
	}
}

func TestT254_5_1InterruptCancelsHandler(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.toolDeadline = 5 * time.Second
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	h := s.boundTool("jevons_agent_start", func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		<-block
		return mcp.NewToolResultText("ok"), nil
	})
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"actor": "jevons-po"}
	done := make(chan *mcp.CallToolResult, 1)
	go func() {
		res, _ := h(context.Background(), req)
		done <- res
	}()
	time.Sleep(30 * time.Millisecond)
	s.cancelMCPFlights("jevons-po")
	select {
	case res := <-done:
		if res == nil || !res.IsError || !strings.Contains(toolText(res), "T254.5.1") {
			t.Fatalf("interrupt result %v", toolText(res))
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("interrupt did not cancel the in-process tools/call (🎯T254.5.1)")
	}
}

func TestT254_5_2SameToolCallUnsticksPO(t *testing.T) {
	t.Parallel()
	o := FleetRecoverObs{
		Name: "jevons-po", Purpose: claudia.PurposeWork, ProcessRunning: true,
		HasOpenMission: true, PromptInFlight: true,
		SinceProgress: 2 * time.Second, StuckTimeout: 90 * time.Second,
		Phase: "working", SpawnClass: true,
		SameToolID: "tool_8e5db3f3", SameToolSince: 2 * time.Minute,
	}
	act, reason := ClassifyFleetRecover(o)
	if act != FleetRecoverUnstick || reason != "stuck_busy_same_tool" {
		t.Fatalf("got %s/%s want unstick/stuck_busy_same_tool", act, reason)
	}
}

func TestT254_5_2WorkerChangingToolsNotUnstuck(t *testing.T) {
	t.Parallel()
	o := FleetRecoverObs{
		Name: "jv-t236-worker", Purpose: claudia.PurposeWork, ProcessRunning: true,
		HasOpenMission: true, PromptInFlight: true,
		SinceProgress: 5 * time.Second, StuckTimeout: 90 * time.Second,
		Phase: "working", SpawnClass: false,
		SameToolID: "tool_aaaa", SameToolSince: 2 * time.Minute,
	}
	act, reason := ClassifyFleetRecover(o)
	if act != FleetRecoverSkip {
		t.Fatalf("worker same-id with fresh progress: %s/%s want skip", act, reason)
	}
}

func TestT254_5_2MissionlessIdlePONotThrashed(t *testing.T) {
	t.Parallel()
	o := FleetRecoverObs{
		Name: "jevons-po", Purpose: claudia.PurposeWork, ProcessRunning: true,
		HasOpenMission: true, PromptInFlight: false, Phase: "idle",
		SpawnClass: true, SinceProgress: 10 * time.Minute,
	}
	act, reason := ClassifyFleetRecover(o)
	if act != FleetRecoverSkip {
		t.Fatalf("idle PO: %s/%s want skip", act, reason)
	}
}

func TestT254_5_2SameToolIDHeartbeatsDoNotResetSince(t *testing.T) {
	tr := NewIdleActivityTracker()
	t0 := time.Unix(1_700_000_000, 0)
	now := t0
	tr.now = func() time.Time { return now }
	raw := []byte(`{"type":"progress","progress_type":"tool_use","raw":{"update":{"sessionUpdate":"tool_call_update","status":"in_progress","toolCallId":"tool_8e5db3f3"}}}`)
	ev := claudia.Event{Type: "progress", ProgressType: "tool_use", Raw: raw}
	tr.Observe("jevons-po", ev)
	first := tr.Get("jevons-po")
	now = t0.Add(2 * time.Minute)
	tr.Observe("jevons-po", ev)
	second := tr.Get("jevons-po")
	if first.ToolCallID != "tool_8e5db3f3" || second.ToolCallID != first.ToolCallID {
		t.Fatalf("tool id %q → %q", first.ToolCallID, second.ToolCallID)
	}
	if !second.ToolCallSince.Equal(first.ToolCallSince) {
		t.Fatal("same tool_call_id heartbeat reset ToolCallSince (🎯T254.5.2)")
	}
	if !second.Updated.After(first.Updated) {
		t.Fatal("Updated should still refresh for worker T236 heartbeats")
	}
}
