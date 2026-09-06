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

func TestT541_2HandleAgentStartLaunchDeadlineReleasesMutex(t *testing.T) {
	s, _ := t541Server(t)
	s.launchDeadline = 50 * time.Millisecond
	cleaned := make(chan struct{})
	s.launchAgentFn = func(ctx context.Context, _ string) (*claudia.Agent, error) {
		<-ctx.Done()
		close(cleaned)
		return nil, ctx.Err()
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":     "jv-t541.2-hang",
		"workdir":  t.TempDir(),
		"provider": string(claudia.ProviderCursor),
		"parent":   "jevons-po",
		"purpose":  "work",
	}
	t.Cleanup(func() {
		select {
		case <-cleaned:
		default:
			t.Error("startup cleanup was abandoned")
		}
	})
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
	// A later start can take startMu immediately after cleanup completed.
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
	cleaned := make(chan struct{})
	s.launchAgentFn = func(ctx context.Context, _ string) (*claudia.Agent, error) {
		<-ctx.Done()
		close(cleaned)
		return nil, ctx.Err()
	}
	t.Cleanup(func() {
		select {
		case <-cleaned:
		default:
			t.Error("startup cleanup was abandoned")
		}
	})
	start := time.Now()
	_, err := s.launchAgentBounded(t.Context(), "jv-t541.2")
	if err == nil || !strings.Contains(err.Error(), "T541.2") {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) > 400*time.Millisecond {
		t.Fatalf("bounded launch waited %s", time.Since(start))
	}
}

func TestT541_2LaunchDeadlinePreservesExistingHandle(t *testing.T) {
	s, _ := t541Server(t)
	s.launchDeadline = time.Millisecond
	existing := &claudia.Agent{}
	s.launchAgentFn = func(ctx context.Context, _ string) (*claudia.Agent, error) {
		// A legacy registry lock or an existing handle's Alive probe can
		// finish after cancellation. This caller owns neither process.
		<-ctx.Done()
		return existing, nil
	}
	got, err := s.launchAgentBounded(t.Context(), "already-running")
	if err != nil || got != existing {
		t.Fatalf("existing handle discarded after deadline: got=%p err=%v", got, err)
	}
}
