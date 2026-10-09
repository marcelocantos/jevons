// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
	req2 := httptest.NewRequest(http.MethodGet, "/api/logs?source=browser&decision=match&limit=5", nil)
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

func TestLogsAPIPagesFiltersAndStreams(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	j, err := eventlog.Open(eventlog.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	s.SetEventLog(j)
	for _, ev := range []eventlog.Event{
		{TS: "2026-09-27T10:00:00Z", Source: "server", Level: "info", Component: "route", Decision: "send", Msg: "sent alpha"},
		{TS: "2026-09-27T10:00:01Z", Source: "browser", Level: "debug", Component: "history", Msg: "hydrate page"},
		{TS: "2026-09-27T10:00:02Z", Source: "server", Level: "warn", Component: "route", Decision: "retry", Msg: "retry beta"},
	} {
		if err := j.Append(ev); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	get := func(path string) (int, struct {
		Events       []eventlog.Event `json:"events"`
		NextCursor   *int64           `json:"next_cursor"`
		HeadCursor   int64            `json:"head_cursor"`
		ScannedBytes int64            `json:"scanned_bytes"`
	}) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var payload struct {
			Events       []eventlog.Event `json:"events"`
			NextCursor   *int64           `json:"next_cursor"`
			HeadCursor   int64            `json:"head_cursor"`
			ScannedBytes int64            `json:"scanned_bytes"`
		}
		if rec.Code == http.StatusOK && json.Unmarshal(rec.Body.Bytes(), &payload) != nil {
			t.Fatalf("bad response: %s", rec.Body.String())
		}
		return rec.Code, payload
	}
	status, first := get("/api/logs?limit=1")
	if status != http.StatusOK || len(first.Events) != 1 || first.Events[0].Msg != "retry beta" || first.NextCursor == nil || first.HeadCursor <= 0 {
		t.Fatalf("first page: %d %+v", status, first)
	}
	status, older := get(fmt.Sprintf("/api/logs?limit=1&before=%d", *first.NextCursor))
	if status != http.StatusOK || len(older.Events) != 1 || older.Events[0].Msg != "sent alpha" {
		t.Fatalf("older page: %d %+v", status, older)
	}
	status, browser := get("/api/logs?source=browser&level=debug&q=HYDRATE&since=2026-09-27T10:00:01Z")
	if status != http.StatusOK || len(browser.Events) != 1 || browser.Events[0].Msg != "hydrate page" {
		t.Fatalf("browser filter: %d %+v", status, browser)
	}
	for _, path := range []string{"/api/logs?level=nope", "/api/logs?limit=-1", "/api/logs?before=bogus", "/api/logs?unknown=yes"} {
		if status, _ := get(path); status != http.StatusBadRequest {
			t.Errorf("%s status %d, want 400", path, status)
		}
	}

	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/logs/stream?after=%d", server.URL, first.HeadCursor), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream response %s", response.Status)
	}
	if err := j.Append(eventlog.Event{Source: "browser", Level: "debug", Msg: "ignore me"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(eventlog.Event{Source: "server", Level: "error", Msg: "new failure"}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	var gotID int64
	var gotEvent eventlog.Event
	for gotEvent.Msg == "" {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream: %v", err)
		}
		if strings.HasPrefix(line, "id: ") {
			gotID, err = strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "id: ")), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
		if strings.HasPrefix(line, "data: ") {
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &gotEvent); err != nil {
				t.Fatal(err)
			}
		}
	}
	if gotEvent.Msg != "new failure" || gotID <= first.HeadCursor {
		t.Fatalf("stream id=%d event=%+v", gotID, gotEvent)
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

// 🎯T411 persist path: browser hydrate_page still lands in events.jsonl
// (clause 4 — do not drop evidence), but the default GET /api/logs window
// is source=server so those debug rows cannot hide a delivery.
func TestT411BrowserHydratePersistDoesNotHideServerDecisions(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	j, err := eventlog.Open(eventlog.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	s.SetEventLog(j)
	s.LogEvent("route", "send", map[string]any{"msg": "route send jevons-po delivered"})

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	hydrate, _ := json.Marshal(map[string]any{
		"level": "debug",
		"msg":   "hydrate page",
		"fields": map[string]any{
			"component": "history",
			"decision":  "hydrate_page",
		},
	})
	const nHydrate = 40
	for i := 0; i < nHydrate; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/log", bytes.NewReader(hydrate))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost")
		req.Host = "localhost"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("POST /api/log status %d", rr.Code)
		}
	}

	get := func(path string) (int, []eventlog.Event) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Origin", "http://localhost")
		req.Host = "localhost"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status %d body=%s", path, rr.Code, rr.Body.String())
		}
		var payload struct {
			Events []eventlog.Event `json:"events"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return len(payload.Events), payload.Events
	}

	n, events := get("/api/logs?limit=100")
	if n == 0 {
		t.Fatal("default /api/logs empty after hydrate persist — server decisions hidden")
	}
	found := false
	for _, ev := range events {
		if ev.Source != "server" {
			t.Fatalf("default /api/logs leaked a browser row: %+v", ev)
		}
		if strings.Contains(ev.Msg, "jevons-po") {
			found = true
		}
	}
	if !found {
		t.Fatalf("default /api/logs missing jevons-po delivery: %+v", events)
	}

	n, events = get("/api/logs?source=browser&limit=100")
	if n != nHydrate {
		t.Fatalf("source=browser count=%d want %d — persist must keep hydrate rows", n, nHydrate)
	}
	for _, ev := range events {
		if ev.Source != "browser" || ev.Decision != "hydrate_page" {
			t.Fatalf("browser row=%+v", ev)
		}
	}
}
