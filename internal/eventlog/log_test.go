// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
)

type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler       { return h }

func TestLogDualWriteServerSource(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })

	if err := Log(j, "agent_lifecycle", "start", map[string]any{
		"name":   "worker-a",
		"parent": "jevons-po",
		"corr":   "c-1",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	if len(cap.records) != 1 {
		t.Fatalf("slog records=%d want 1", len(cap.records))
	}
	got := map[string]any{}
	cap.records[0].Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.Any()
		return true
	})
	if got["component"] != "agent_lifecycle" || got["decision"] != "start" || got["source"] != "server" {
		t.Fatalf("slog attrs=%v", got)
	}
	if got["name"] != "worker-a" || got["corr"] != "c-1" {
		t.Fatalf("slog fields=%v", got)
	}

	events, err := Tail(path, TailOptions{Limit: 10, Component: "agent_lifecycle", Source: "server"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("journal events=%d", len(events))
	}
	ev := events[0]
	if ev.Source != "server" || ev.Component != "agent_lifecycle" || ev.Decision != "start" {
		t.Fatalf("event=%+v", ev)
	}
	if ev.Corr != "c-1" {
		t.Fatalf("corr=%q", ev.Corr)
	}
	if ev.Fields["name"] != "worker-a" || ev.Fields["parent"] != "jevons-po" {
		t.Fatalf("fields=%v", ev.Fields)
	}
	// First-class keys must not be duplicated into Fields.
	if _, ok := ev.Fields["component"]; ok {
		t.Fatal("component leaked into Fields")
	}
	if _, ok := ev.Fields["corr"]; ok {
		t.Fatal("corr leaked into Fields")
	}
}

func TestLogNilJournalSlogOnly(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if err := Log(nil, "notify_queue", "defer", map[string]any{"depth": 2}); err != nil {
		t.Fatalf("Log nil journal: %v", err)
	}
	if len(cap.records) != 1 {
		t.Fatalf("records=%d", len(cap.records))
	}
}

func TestLogLevelWarn(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "e.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })

	if err := Log(j, "agent_transcript", "empty", map[string]any{
		"level":        "warn",
		"empty_reason": "no_session",
		"msg":          "soft-empty transcript",
	}); err != nil {
		t.Fatal(err)
	}
	if cap.records[0].Level != slog.LevelWarn {
		t.Fatalf("level=%v", cap.records[0].Level)
	}
	evs, err := Tail(path, TailOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if evs[0].Level != "warn" || evs[0].Msg != "soft-empty transcript" {
		t.Fatalf("ev=%+v", evs[0])
	}
	if evs[0].Fields["empty_reason"] != "no_session" {
		t.Fatalf("fields=%v", evs[0].Fields)
	}
}

func TestLogEventMultiComponentFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	LogEvent(j, "info", "agent_lifecycle", "start", "agent_lifecycle.start", map[string]any{
		"name": "worker", "outcome": "ok", "parent": "jevons-po",
	})
	LogEvent(j, "info", "thread", "spawn", "thread.spawn", map[string]any{
		"id": "aside-1", "outcome": "ok", "parent": "jevons",
	})
	LogEvent(j, "info", "event_push", "push", "event_push.push", map[string]any{
		"target": "jevons-po", "outcome": "ok", "event": "timer",
	})
	// Nil journal still slogs.
	LogEvent(nil, "info", "agent_lifecycle", "stop", "agent_lifecycle.stop", map[string]any{
		"name": "worker", "outcome": "ok",
	})

	if len(cap.records) < 4 {
		t.Fatalf("slog records=%d want ≥4", len(cap.records))
	}
	got := map[string]any{}
	cap.records[0].Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.Any()
		return true
	})
	if got["component"] != "agent_lifecycle" || got["outcome"] != "ok" || got["decision"] != "start" {
		t.Fatalf("first slog attrs=%v", got)
	}
	if got["source"] != "server" {
		t.Fatalf("source=%v", got["source"])
	}

	byLife, err := Tail(path, TailOptions{Component: "agent_lifecycle", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(byLife) != 1 {
		t.Fatalf("agent_lifecycle filter count=%d want 1 (nil-journal stop not on disk)", len(byLife))
	}
	if byLife[0].Source != "server" || byLife[0].Fields["outcome"] != "ok" {
		t.Fatalf("life event=%+v fields=%v", byLife[0], byLife[0].Fields)
	}

	byThread, err := Tail(path, TailOptions{Component: "thread", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(byThread) != 1 || byThread[0].Decision != "spawn" {
		t.Fatalf("thread=%+v", byThread)
	}

	byPush, err := Tail(path, TailOptions{Component: "event_push", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(byPush) != 1 {
		t.Fatalf("event_push count=%d", len(byPush))
	}

	// source=server filter (acceptance: GET /api/logs style).
	srv, err := Tail(path, TailOptions{Source: "server", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(srv) != 3 {
		t.Fatalf("source=server count=%d want 3", len(srv))
	}
}
