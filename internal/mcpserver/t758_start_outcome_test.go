// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T758: two overlapping starts under one name run the launch once and both
// callers see the same outcome (never a second executor).
func TestT758OverlappingStartsShareOneLaunch(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.toolDeadline = 5 * time.Second
	release := make(chan struct{})
	var calls atomic.Int32
	inner := s.detachStart(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		<-release
		return mcp.NewToolResultText("outcome=" + StartOutcomeStarted + ": Agent \"jv-t758\" started (session: sess-1)"), nil
	})
	first := make(chan *mcp.CallToolResult, 1)
	go func() { r, _ := inner(context.Background(), startReq("jv-t758")); first <- r }()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	second := make(chan *mcp.CallToolResult, 1)
	go func() { r, _ := inner(context.Background(), startReq("jv-t758")); second <- r }()
	time.Sleep(30 * time.Millisecond)
	close(release)
	var sessionIDs []string
	for _, ch := range []chan *mcp.CallToolResult{first, second} {
		select {
		case r := <-ch:
			got := toolText(r)
			if !strings.Contains(got, "outcome="+StartOutcomeStarted) {
				t.Fatalf("missing outcome=started: %q", got)
			}
			if !strings.Contains(got, "session: sess-1") {
				t.Fatalf("callers must share one session id, got %q", got)
			}
			sessionIDs = append(sessionIDs, "sess-1")
		case <-time.After(2 * time.Second):
			t.Fatal("caller never got the outcome")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("launch ran %d times, want exactly 1 executor", n)
	}
	if sessionIDs[0] != sessionIDs[1] {
		t.Fatalf("session ids diverged: %v", sessionIDs)
	}
}

// 🎯T758: a same-name start against an already-running seat reports
// outcome=started without launching again or re-delivering a brief.
func TestT758AlreadyRunningSameNameIsNoOp(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	const name = "jv-t758-live"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "sess-live-1",
		Provider: claudia.ProviderClaude, Parent: "jevons-po", Purpose: claudia.PurposeWork,
		Materialized: true,
	}); err != nil {
		t.Fatal(err)
	}
	s.Seats().FromClaudia(seatstate.SeatReport{
		Name: name, Alive: true, Known: true, Provider: "claude", SessionID: "sess-live-1",
	}, time.Now())

	var launches atomic.Int32
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) {
		launches.Add(1)
		return nil, nil
	}
	var briefs atomic.Int32
	s.SetTurnWitness(func(string, string) turnWatch {
		briefs.Add(1)
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, SessionEvent: true, Detail: "should not run"}
		}
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": name, "workdir": dir, "provider": string(claudia.ProviderClaude),
		"purpose": "work", "parent": "jevons-po",
		"prompt": "Do not re-deliver this brief.",
	}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("already-running start failed: %s", toolText(res))
	}
	got := toolText(res)
	if !strings.Contains(got, "outcome="+StartOutcomeStarted) {
		t.Fatalf("missing outcome=started: %s", got)
	}
	if !strings.Contains(got, "sess-live-1") {
		t.Fatalf("must report the existing session: %s", got)
	}
	if !strings.Contains(got, "already running") || !strings.Contains(got, "T758") {
		t.Fatalf("must name the no-op: %s", got)
	}
	if strings.Contains(got, "prompt_delivered=true") {
		t.Fatalf("must not claim a re-brief: %s", got)
	}
	if launches.Load() != 0 {
		t.Fatalf("launched %d times; already-running must not launch", launches.Load())
	}
	if briefs.Load() != 0 {
		t.Fatalf("brief path ran %d times; already-running must not re-brief", briefs.Load())
	}
}

// 🎯T758: a genuine pre-effect refusal names outcome=not-started.
func TestT758NotStartedOutcomeOnMissingArgs(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "x"}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected error")
	}
	got := toolText(res)
	if !strings.Contains(got, "outcome="+StartOutcomeNotStarted) {
		t.Fatalf("want outcome=not-started, got %q", got)
	}
}
