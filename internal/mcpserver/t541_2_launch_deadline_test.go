// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestT541_2HandleAgentStartLaunchDeadlineReleasesMutex(t *testing.T) {
	s, _ := t541Server(t)
	s.launchDeadline = 50 * time.Millisecond
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	s.launchAgentFn = func(string) (*claudia.Agent, error) {
		<-block
		return nil, nil
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":     "jv-t541.2-hang",
		"workdir":  t.TempDir(),
		"provider": string(claudia.ProviderCursor),
		"parent":   "jevons-po",
		"purpose":  "work",
	}
	start := time.Now()
	res, err := s.handleAgentStart(t.Context(), req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("hung launch must return an MCP error")
	}
	text := toolText(res)
	if !strings.Contains(text, "timed out") || !strings.Contains(text, "T541.2") {
		t.Fatalf("error %q should name the T541.2 deadline", text)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("deadline waited %s, want ~50ms", elapsed)
	}
	if s.startMutexHeld() {
		t.Fatal("start mutex still held after launch timeout")
	}
	// A later start must be able to take startMu immediately — the abandoned
	// Launch goroutine must not keep the mutex.
	taken := make(chan struct{})
	go func() {
		s.startMu.Lock()
		close(taken)
		s.startMu.Unlock()
	}()
	select {
	case <-taken:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("startMu still contended after launch timeout")
	}
}

func TestT541_2LaunchAgentBoundedTimesOut(t *testing.T) {
	s, _ := t541Server(t)
	s.launchDeadline = 40 * time.Millisecond
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	s.launchAgentFn = func(string) (*claudia.Agent, error) {
		<-block
		return nil, nil
	}
	start := time.Now()
	_, err := s.launchAgentBounded(t.Context(), "jv-t541.2")
	if err == nil || !strings.Contains(err.Error(), "T541.2") {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Fatalf("bounded launch waited %s", time.Since(start))
	}
}
