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
)

func startReq(name string) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": name, "workdir": "/tmp/x"}
	return req
}

// 🎯T792: a start slower than the caller's deadline still completes.
func TestT792StartSurvivesCallerDeadline(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.toolDeadline = 30 * time.Millisecond
	release := make(chan struct{})
	var launched, briefed, ctxCancelled atomic.Bool
	done := make(chan struct{})
	h := s.boundTool("jevons_agent_start", s.detachStart(func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		<-release
		ctxCancelled.Store(ctx.Err() != nil)
		launched.Store(true)
		briefed.Store(true)
		close(done)
		return mcp.NewToolResultText("started"), nil
	}))

	res, err := h(context.Background(), startReq("jv-slow"))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !strings.Contains(toolText(res), "T792") {
		t.Fatalf("caller must be told the launch continues (T792), got %q", toolText(res))
	}
	if launched.Load() {
		t.Fatal("launch should still be in flight")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("launch was lost with the caller deadline")
	}
	if ctxCancelled.Load() {
		t.Fatal("launch context was cancelled by the caller deadline")
	}
	if !launched.Load() || !briefed.Load() {
		t.Fatal("launch or brief not completed")
	}
}

// A retry under the same name joins the in-flight launch rather than
// launching a second time, and sees the first launch's outcome.
func TestT792RetrySameNameJoinsInFlightLaunch(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.toolDeadline = 5 * time.Second
	release := make(chan struct{})
	var calls atomic.Int32
	inner := s.detachStart(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		<-release
		return mcp.NewToolResultText("started jv-slow"), nil
	})
	first := make(chan *mcp.CallToolResult, 1)
	go func() { r, _ := inner(context.Background(), startReq("jv-slow")); first <- r }()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	second := make(chan *mcp.CallToolResult, 1)
	go func() { r, _ := inner(context.Background(), startReq("jv-slow")); second <- r }()
	time.Sleep(50 * time.Millisecond)
	close(release)
	for _, ch := range []chan *mcp.CallToolResult{first, second} {
		select {
		case r := <-ch:
			if !strings.Contains(toolText(r), "started jv-slow") {
				t.Fatalf("got %q", toolText(r))
			}
		case <-time.After(2 * time.Second):
			t.Fatal("caller never got the outcome")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("launch ran %d times, want 1 (double launch)", n)
	}
}

// A different name is not blocked by an in-flight launch.
func TestT792DifferentNamesLaunchIndependently(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var calls atomic.Int32
	inner := s.detachStart(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		<-release
		return mcp.NewToolResultText("ok"), nil
	})
	go inner(context.Background(), startReq("a"))
	go inner(context.Background(), startReq("b"))
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2", calls.Load())
	}
}
