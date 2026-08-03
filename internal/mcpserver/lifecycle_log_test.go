// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// findLifecycle finds the first slog record with the given component+decision.
func findLifecycle(records []slog.Record, component, decision string) map[string]any {
	for _, r := range records {
		m := attrsMap(r)
		if m["component"] == component && m["decision"] == decision {
			return m
		}
	}
	return nil
}

// 🎯T128.1: agent_stop success emits component + outcome on slog and journal.
func TestAgentStopLifecycleLog(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })

	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "worker", WorkDir: dir, SessionID: "s1", Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}

	s := &Server{registry: reg}
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		_ = eventlog.Log(j, component, decision, fields)
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker"}
	res, err := s.handleAgentStop(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("stop: %s", toolText(res))
	}

	got := findLifecycle(cap.records, compAgentLifecycle, "stop")
	if got == nil {
		t.Fatalf("no agent_lifecycle stop slog; records=%d", len(cap.records))
	}
	if got["outcome"] != "ok" || got["name"] != "worker" {
		t.Fatalf("slog attrs=%v", got)
	}

	evs, err := eventlog.Tail(path, eventlog.TailOptions{Component: "agent_lifecycle", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) < 1 {
		t.Fatal("expected journal Append for agent_stop")
	}
	if evs[0].Decision != "stop" || evs[0].Source != "server" {
		t.Fatalf("event=%+v", evs[0])
	}
	if evs[0].Fields["outcome"] != "ok" {
		t.Fatalf("fields=%v", evs[0].Fields)
	}
}

// 🎯T128.1: agent_kill success path structured attrs.
func TestAgentKillLifecycleLog(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "po"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("kill: %s", toolText(res))
	}
	got := findLifecycle(cap.records, compAgentLifecycle, "kill")
	if got == nil {
		t.Fatal("expected kill lifecycle slog")
	}
	if got["outcome"] != "ok" || got["name"] != "worker" || got["actor"] != "po" {
		t.Fatalf("attrs=%v", got)
	}
}

// 🎯T128.1: agent_kill denied still logs outcome=error.
func TestAgentKillDeniedLifecycleLog(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	reg := regWithTree(t)
	s := &Server{registry: reg}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "peer"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected deny")
	}
	got := findLifecycle(cap.records, compAgentLifecycle, "kill")
	if got == nil || got["outcome"] != "error" {
		t.Fatalf("attrs=%v", got)
	}
}

// 🎯T128.1: agent_start validation failure logs component+outcome (no Launch).
func TestAgentStartErrorLifecycleLog(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := &Server{}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "x", "workdir": "/tmp", "parent": "x"}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected parent==name error")
	}
	got := findLifecycle(cap.records, compAgentLifecycle, "start")
	if got == nil || got["outcome"] != "error" {
		t.Fatalf("attrs=%v", got)
	}
	if got["err"] != "parent_equals_name" {
		t.Fatalf("err attr=%v", got["err"])
	}
}

// 🎯T128.1: thread_spawn success via fake fleet + journal filter.
func TestThreadSpawnLifecycleLog(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })

	dir := t.TempDir()
	ff := &pushFakeFleet{mintID: "sess-spawn"}
	b := newPushButler(t, dir, ff, nil)

	s := &Server{butler: b}
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		_ = eventlog.Log(j, component, decision, fields)
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"id": "aside-a", "workdir": dir, "actor": "jevons-po",
	}
	res, err := s.handleThreadSpawn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("spawn: %s", toolText(res))
	}

	got := findLifecycle(cap.records, compThread, "spawn")
	if got == nil {
		t.Fatal("expected thread spawn slog")
	}
	if got["outcome"] != "ok" || got["id"] != "aside-a" || got["parent"] != "jevons-po" {
		t.Fatalf("attrs=%v", got)
	}

	evs, err := eventlog.Tail(path, eventlog.TailOptions{Component: "thread", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Decision != "spawn" || evs[0].Source != "server" {
		t.Fatalf("journal=%+v", evs)
	}
}

// 🎯T128.1: event_push success + component filter for logs_tail /api/logs.
func TestEventPushLifecycleLog(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })

	dir := t.TempDir()
	p := &pushFakeParticipants{names: map[string]bool{"jevons-po": true}}
	b := newPushButler(t, dir, &pushFakeFleet{}, p)

	s := &Server{butler: b}
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		_ = eventlog.Log(j, component, decision, fields)
	})

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"target": "jevons-po", "event": "timer", "text": "tick",
	}
	res, err := s.handleEventPush(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("push: %s", toolText(res))
	}

	got := findLifecycle(cap.records, compEventPush, "push")
	if got == nil || got["outcome"] != "ok" || got["target"] != "jevons-po" {
		t.Fatalf("attrs=%v", got)
	}

	// Filter surface used by GET /api/logs and jevons_logs_tail.
	for _, comp := range []string{"event_push"} {
		evs, err := eventlog.Tail(path, eventlog.TailOptions{Component: comp, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) < 1 {
			t.Fatalf("component=%s filter empty", comp)
		}
	}
}

// 🎯T128.1: event_push failure logs outcome=error.
func TestEventPushErrorLifecycleLog(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := t.TempDir()
	b := newPushButler(t, dir, &pushFakeFleet{}, &pushFakeParticipants{names: map[string]bool{}})
	s := &Server{butler: b}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"target": "ghost", "event": "ci", "text": "green",
	}
	res, err := s.handleEventPush(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected error")
	}
	got := findLifecycle(cap.records, compEventPush, "push")
	if got == nil || got["outcome"] != "error" {
		t.Fatalf("attrs=%v", got)
	}
}

// 🎯T128.1: multi-component journal filter (api/logs oracle).
func TestLifecycleComponentsFilterable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })

	s := &Server{}
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		_ = eventlog.Log(j, component, decision, fields)
	})
	s.logLifecycle(compAgentLifecycle, "start", "ok", map[string]any{"name": "a"})
	s.logLifecycle(compThread, "spawn", "ok", map[string]any{"id": "t"})
	s.logLifecycle(compEventPush, "push", "ok", map[string]any{"target": "a"})

	for _, comp := range []string{"agent_lifecycle", "thread", "event_push"} {
		evs, err := eventlog.Tail(path, eventlog.TailOptions{Component: comp, Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		if len(evs) != 1 {
			t.Fatalf("component=%s count=%d", comp, len(evs))
		}
		if evs[0].Source != "server" {
			t.Fatalf("source=%s", evs[0].Source)
		}
		if evs[0].Fields["outcome"] != "ok" {
			t.Fatalf("outcome missing for %s: %v", comp, evs[0].Fields)
		}
	}
}
