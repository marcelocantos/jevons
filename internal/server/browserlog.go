// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
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

// handleLogsTail serves GET /api/logs?limit=&component=&decision=&source=&q=
// — newest-first events from the durable journal (product introspection).
func (s *Server) handleLogsTail(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSite(w, r) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	path := s.eventJournalPath()
	events, err := eventlog.Tail(path, eventlog.TailOptions{
		Limit:     limit,
		Component: r.URL.Query().Get("component"),
		Decision:  r.URL.Query().Get("decision"),
		Source:    r.URL.Query().Get("source"),
		Contains:  r.URL.Query().Get("q"),
	})
	if err != nil {
		http.Error(w, `{"error":"log tail failed"}`, http.StatusInternalServerError)
		return
	}
	if events == nil {
		events = []eventlog.Event{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"path":   path,
		"count":  len(events),
		"events": events,
	})
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
