// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// handleBrowserLog accepts log events from the browser, appends them to
// the durable event journal under state_dir/logs/, and mirrors them to
// slog. The browser must not be a black box: decisions and lifecycle
// events leave via this ingest path so the overseer can read files
// without a special privilege model (🎯T120).
//
// Request body:
//
//	{
//	  "level":  "info" | "warn" | "error" | "debug",
//	  "msg":    "decision.route",
//	  "fields": { "component": "thread_route", "decision": "match", "corr": "…", … }
//	}
//
// Always returns 204 — log delivery is best-effort for the browser UI.
func (s *Server) handleBrowserLog(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSite(w, r) {
		return
	}
	var entry struct {
		Level  string         `json:"level"`
		Msg    string         `json:"msg"`
		Fields map[string]any `json:"fields,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	level := strings.ToLower(strings.TrimSpace(entry.Level))
	if level == "" {
		level = "info"
	}
	msg := entry.Msg
	if msg == "" {
		msg = "browser log"
	}

	component := "browser"
	var decision, corr string
	rest := map[string]any{}
	for k, v := range entry.Fields {
		switch k {
		case "component":
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				component = s
			}
		case "decision":
			if s, ok := v.(string); ok {
				decision = s
			}
		case "corr":
			if s, ok := v.(string); ok && s != "" {
				corr = s
			}
		default:
			rest[k] = v
		}
	}

	// Durable file under ~/.jevons/logs/events.jsonl (state_dir).
	if j := s.eventJournal(); j != nil {
		ev := eventlog.Event{
			TS:        time.Now().UTC().Format(time.RFC3339Nano),
			Source:    "browser",
			Level:     level,
			Msg:       msg,
			Component: component,
			Decision:  decision,
			Corr:      corr,
		}
		if len(rest) > 0 {
			ev.Fields = rest
		}
		if err := j.Append(ev); err != nil {
			slog.Warn("eventlog: browser append failed", "err", err, "path", j.Path())
		}
	}

	// slog mirror for live tail of process logs.
	args := []any{"component", component, "level", level, "msg", msg, "source", "browser"}
	if decision != "" {
		args = append(args, "decision", decision)
	}
	if corr != "" {
		args = append(args, "corr", corr)
	}
	for k, v := range rest {
		args = append(args, k, v)
	}
	logMsg := "browser: " + msg
	switch level {
	case "error":
		slog.Error(logMsg, args...)
	case "warn":
		slog.Warn(logMsg, args...)
	case "debug":
		slog.Debug(logMsg, args...)
	default:
		slog.Info(logMsg, args...)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLogsTail pages backward through the durable journal. The default
// source is server so browser hydration telemetry cannot hide decisions.
func (s *Server) handleLogsTail(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSite(w, r) {
		return
	}
	query, err := parseLogQuery(r, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > eventlog.MaxPageEvents {
			http.Error(w, "limit must be 1..2000", http.StatusBadRequest)
			return
		}
	}
	var before *int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		cursor, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || cursor < 0 {
			http.Error(w, "invalid before cursor", http.StatusBadRequest)
			return
		}
		before = &cursor
	}
	path := s.eventJournalPath()
	page, err := eventlog.Page(path, before, limit, query)
	if err != nil {
		if errors.Is(err, eventlog.ErrCursor) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, `{"error":"log tail failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"path":          path,
		"count":         len(page.Events),
		"events":        page.Events,
		"next_cursor":   page.NextCursor,
		"head_cursor":   page.HeadCursor,
		"scanned_bytes": page.ScannedBytes,
	})
}

func parseLogQuery(r *http.Request, stream bool) (eventlog.Query, error) {
	allowed := map[string]bool{"component": true, "decision": true, "source": true, "level": true, "q": true, "since": true, "until": true}
	if stream {
		allowed["after"] = true
	} else {
		allowed["before"] = true
		allowed["limit"] = true
	}
	for key, values := range r.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			return eventlog.Query{}, fmt.Errorf("unsupported or repeated log filter %q", key)
		}
		if len(values[0]) > 256 {
			return eventlog.Query{}, fmt.Errorf("log filter %q is too long", key)
		}
	}
	values := r.URL.Query()
	query := eventlog.Query{
		Component: strings.TrimSpace(values.Get("component")),
		Decision:  strings.TrimSpace(values.Get("decision")),
		Source:    strings.TrimSpace(values.Get("source")),
		Level:     strings.TrimSpace(values.Get("level")),
		Contains:  strings.TrimSpace(values.Get("q")),
	}
	if query.Source == "" {
		query.Source = "server"
	}
	if query.Source == "all" {
		query.Source = ""
	} else if query.Source != "server" && query.Source != "browser" {
		return eventlog.Query{}, errors.New("source must be server, browser, or all")
	}
	if query.Level != "" && query.Level != "debug" && query.Level != "info" && query.Level != "warn" && query.Level != "error" {
		return eventlog.Query{}, errors.New("level must be debug, info, warn, or error")
	}
	for _, bound := range []struct {
		name string
		dest *time.Time
	}{{"since", &query.Since}, {"until", &query.Until}} {
		if raw := values.Get(bound.name); raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return eventlog.Query{}, fmt.Errorf("%s must be RFC3339", bound.name)
			}
			*bound.dest = parsed
		}
	}
	if !query.Since.IsZero() && !query.Until.IsZero() && query.Since.After(query.Until) {
		return eventlog.Query{}, errors.New("since must not be after until")
	}
	return query, nil
}

// handleLogsStream sends new matching rows as Server-Sent Events. Each id is
// the byte offset after that row; reconnect with Last-Event-ID or ?after=.
func (s *Server) handleLogsStream(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSite(w, r) {
		return
	}
	query, err := parseLogQuery(r, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := s.eventJournalPath()
	var after int64
	raw := r.URL.Query().Get("after")
	if raw == "" {
		raw = r.Header.Get("Last-Event-ID")
	}
	if raw == "" {
		info, err := os.Stat(path)
		if err == nil {
			after = info.Size()
		} else if !os.IsNotExist(err) {
			http.Error(w, "log stream unavailable", http.StatusInternalServerError)
			return
		}
	} else {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			http.Error(w, "invalid after cursor", http.StatusBadRequest)
			return
		}
	}
	// Reject a stale cursor before committing the streaming response.
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, "log stream unavailable", http.StatusInternalServerError)
		return
	}
	if info == nil && after != 0 || info != nil && after > info.Size() {
		http.Error(w, "eventlog: cursor out of range", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	if _, err := fmt.Fprint(w, "retry: 1000\n\n"); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastKeepalive := time.Now()
	for {
		page, err := eventlog.ReadAfter(path, after, 100, query)
		if err != nil {
			_, _ = fmt.Fprint(w, "event: error\ndata: log stream unavailable\n\n")
			_ = controller.Flush()
			return
		}
		_ = controller.SetWriteDeadline(time.Now().Add(3 * time.Second))
		for _, row := range page.Events {
			data, err := json.Marshal(row.Event)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: log\ndata: %s\n\n", row.Cursor, data); err != nil {
				return
			}
		}
		if len(page.Events) > 0 && controller.Flush() != nil {
			return
		}
		after = page.NextCursor
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if len(page.Events) == 0 && time.Since(lastKeepalive) >= 15*time.Second {
				_ = controller.SetWriteDeadline(time.Now().Add(3 * time.Second))
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil || controller.Flush() != nil {
					return
				}
				lastKeepalive = time.Now()
			}
		}
	}
}

func (s *Server) eventJournal() *eventlog.Journal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eventLog
}

func (s *Server) eventJournalPath() string {
	s.mu.RLock()
	j := s.eventLog
	dir := s.stateDir
	s.mu.RUnlock()
	if j != nil && j.Path() != "" {
		return j.Path()
	}
	return eventlog.DefaultPath(dir)
}

// SetEventLog attaches the durable product event journal (🎯T120).
func (s *Server) SetEventLog(j *eventlog.Journal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventLog = j
}

// LogEvent dual-writes a server-sourced lifecycle/decision event to slog and
// the durable journal (source=server) when SetEventLog was called.
// Siblings (🎯T128.1–T128.3) and fleet MCP tools use this path so
// GET /api/logs and jevons_logs_tail see fleet truth, not only browser
// decisions (🎯T128.4).
func (s *Server) LogEvent(component, decision string, fields map[string]any) {
	_ = eventlog.Log(s.eventJournal(), component, decision, fields)
}

// EventLogPath returns the durable journal path for MCP/tools.
func (s *Server) EventLogPath() string {
	return s.eventJournalPath()
}

// TailEventLog is the tool-facing tail helper.
func (s *Server) TailEventLog(opt eventlog.TailOptions) ([]eventlog.Event, error) {
	return eventlog.Tail(s.eventJournalPath(), opt)
}
