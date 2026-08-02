// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// captureHandler records slog records for assertions.
type captureHandler struct {
	records []slog.Record
	attrs   []slog.Attr
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := *h
	cp.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &cp
}

func (h *captureHandler) WithGroup(string) slog.Handler { return h }

func TestHandleBrowserLogDurableAndStructured(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := t.TempDir()
	s := New("test", dir)
	j, err := eventlog.Open(eventlog.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	s.SetEventLog(j)

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{
		"level": "info",
		"msg":   "decision.route",
		"fields": map[string]any{
			"component": "thread_route",
			"decision":  "match",
			"threadId":  "att-1",
			"score":     0.9,
			"corr":      "c-9",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/log", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	if len(cap.records) != 1 {
		t.Fatalf("records=%d want 1", len(cap.records))
	}
	got := map[string]any{}
	cap.records[0].Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.Any()
		return true
	})
	if got["component"] != "thread_route" || got["decision"] != "match" || got["corr"] != "c-9" {
		t.Fatalf("attrs=%v", got)
	}

	// Durable journal under state_dir/logs/
	path := filepath.Join(dir, "logs", "events.jsonl")
	events, err := eventlog.Tail(path, eventlog.TailOptions{Limit: 10, Decision: "match"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("durable events=%d path=%s", len(events), path)
	}
	if events[0].Source != "browser" || events[0].Component != "thread_route" {
		t.Fatalf("event=%+v", events[0])
	}

	// GET /api/logs
	req2 := httptest.NewRequest(http.MethodGet, "/api/logs?decision=match&limit=5", nil)
	req2.Header.Set("Origin", "http://localhost")
	req2.Host = "localhost"
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("GET /api/logs status %d", rr2.Code)
	}
	var payload struct {
		Count  int              `json:"count"`
		Events []eventlog.Event `json:"events"`
		Path   string           `json:"path"`
	}
	if err := json.NewDecoder(rr2.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 1 || !strings.Contains(payload.Path, "events.jsonl") {
		t.Fatalf("payload=%+v", payload)
	}
}

// 🎯T128.4: Server.LogEvent dual-writes source=server into the journal and
// surfaces via GET /api/logs?component=agent_lifecycle.
func TestLogEventServerDualWriteAndAPILogs(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := t.TempDir()
	s := New("test", dir)
	j, err := eventlog.Open(eventlog.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	s.SetEventLog(j)

	s.LogEvent("agent_lifecycle", "start", map[string]any{
		"name":   "fleet-child",
		"parent": "jevons-po",
		"ok":     true,
	})

	path := filepath.Join(dir, "logs", "events.jsonl")
	events, err := eventlog.Tail(path, eventlog.TailOptions{
		Limit:     10,
		Component: "agent_lifecycle",
		Source:    "server",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("journal server rows=%d path=%s", len(events), path)
	}
	if events[0].Source != "server" || events[0].Decision != "start" {
		t.Fatalf("event=%+v", events[0])
	}
	if events[0].Fields["name"] != "fleet-child" {
		t.Fatalf("fields=%v", events[0].Fields)
	}

	// slog mirror
	found := false
	for _, r := range cap.records {
		attrs := map[string]any{}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.Any()
			return true
		})
		if attrs["component"] == "agent_lifecycle" && attrs["source"] == "server" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no slog mirror for agent_lifecycle; records=%d", len(cap.records))
	}

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/logs?component=agent_lifecycle&source=server&limit=10", nil)
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/logs status %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Count  int              `json:"count"`
		Events []eventlog.Event `json:"events"`
		Path   string           `json:"path"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 1 || len(payload.Events) != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Events[0].Source != "server" || payload.Events[0].Component != "agent_lifecycle" {
		t.Fatalf("api event=%+v", payload.Events[0])
	}
	if !strings.Contains(payload.Path, "events.jsonl") {
		t.Fatalf("path=%s", payload.Path)
	}
}

// LogEvent with no journal still slog-mirrors (nil journal path).
func TestLogEventWithoutJournal(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := New("test", t.TempDir())
	s.LogEvent("notify_queue", "enqueue", map[string]any{"depth": 1})
	if len(cap.records) == 0 {
		t.Fatal("expected slog record without journal")
	}
}

func TestHandleBrowserLogDefaultComponent(t *testing.T) {
	cap := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{
		"level": "warn",
		"msg":   "fleet refresh failed",
		"fields": map[string]any{
			"err": "network",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/log", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status %d", rr.Code)
	}
	got := map[string]any{}
	cap.records[0].Attrs(func(a slog.Attr) bool {
		got[a.Key] = a.Value.Any()
		return true
	})
	if got["component"] != "browser" {
		t.Fatalf("default component=%v want browser", got["component"])
	}
}
