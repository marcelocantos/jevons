// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/eventlog"
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

// 🎯T128.4: MCP fleet tools call s.LogEvent; with SetEventLogger wired to a
// journal-backed sink, server rows land in events.jsonl.
func TestLogEventViaSetEventLogger(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	j, err := eventlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })

	s := New(t.TempDir(), nil, nil)
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		_ = eventlog.Log(j, component, decision, fields)
	})

	s.LogEvent("agent_lifecycle", "kill", map[string]any{"name": "x", "actor": "jevons-po"})

	evs, err := eventlog.Tail(path, eventlog.TailOptions{
		Limit:     5,
		Component: "agent_lifecycle",
		Source:    "server",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Decision != "kill" {
		t.Fatalf("evs=%+v", evs)
	}
	if evs[0].Fields["name"] != "x" {
		t.Fatalf("fields=%v", evs[0].Fields)
	}
}

func TestLogEventFallbackSlogOnly(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := New(t.TempDir(), nil, nil)
	s.LogEvent("event_push", "ok", map[string]any{"target": "t1"})
	if len(cap.records) == 0 {
		t.Fatal("expected slog fallback without SetEventLogger")
	}
}
