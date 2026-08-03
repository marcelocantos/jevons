// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"log/slog"
	"strings"
	"time"
)

// LogEvent dual-writes a product lifecycle/decision event to slog and,
// when j is non-nil, to the durable journal with source=server (🎯T128.4).
// Prefer this (or Log) over ad-hoc slog for fleet MCP and server P0
// surfaces so GET /api/logs and jevons_logs_tail share one schema.
//
// j may be nil (slog-only). Append failures are best-effort Warn — product
// paths must not fail because logging failed.
//
// component / decision / outcome / corr are promoted to top-level Event and
// slog attrs; remaining fields land under Event.Fields and as slog attrs.
// Reserved field keys "level", "msg", "component", "decision", "source" are
// not duplicated into Fields.
func LogEvent(j *Journal, level, component, decision, msg string, fields map[string]any) {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		level = "info"
	}
	component = strings.TrimSpace(component)
	decision = strings.TrimSpace(decision)

	rest := map[string]any{}
	var outcome, corr string
	for k, v := range fields {
		switch k {
		case "level":
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				level = strings.ToLower(strings.TrimSpace(s))
			}
		case "msg":
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				msg = strings.TrimSpace(s)
			}
		case "component":
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				component = s
			}
		case "decision":
			if s, ok := v.(string); ok {
				decision = s
			}
		case "outcome":
			if s, ok := v.(string); ok {
				outcome = s
			}
		case "corr":
			if s, ok := v.(string); ok && s != "" {
				corr = s
			}
		case "source":
			// Fixed to server for this helper.
		default:
			rest[k] = v
		}
	}
	if outcome != "" {
		rest["outcome"] = outcome
	}
	if component == "" {
		component = "server"
	}
	if msg == "" {
		if decision != "" {
			msg = component + "." + decision
		} else {
			msg = component
		}
	}

	ev := Event{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "server",
		Level:     level,
		Msg:       msg,
		Component: component,
		Decision:  decision,
		Corr:      corr,
	}
	if len(rest) > 0 {
		ev.Fields = rest
	}
	if j != nil {
		if err := j.Append(ev); err != nil {
			slog.Warn("eventlog: server append failed", "err", err, "path", j.Path(), "component", component)
		}
	}

	args := []any{"component", component, "source", "server"}
	if decision != "" {
		args = append(args, "decision", decision)
	}
	if outcome != "" {
		args = append(args, "outcome", outcome)
	}
	if corr != "" {
		args = append(args, "corr", corr)
	}
	for k, v := range rest {
		if k == "outcome" {
			continue
		}
		args = append(args, k, v)
	}

	switch level {
	case "error":
		slog.Error(msg, args...)
	case "warn":
		slog.Warn(msg, args...)
	case "debug":
		slog.Debug(msg, args...)
	default:
		slog.Info(msg, args...)
	}
}

// Log is the short dual-write entrypoint for callers that only have
// component + decision + fields (🎯T128.4). Always returns nil — append
// failures are best-effort Warn inside LogEvent.
func Log(j *Journal, component, decision string, fields map[string]any) error {
	level := "info"
	msg := ""
	if fields != nil {
		if s, ok := fields["level"].(string); ok && strings.TrimSpace(s) != "" {
			level = s
		}
		if s, ok := fields["msg"].(string); ok {
			msg = strings.TrimSpace(s)
		}
	}
	LogEvent(j, level, component, decision, msg, fields)
	return nil
}

// Info is Log at the default info level (fields may still override level).
func Info(j *Journal, component, decision string, fields map[string]any) error {
	return Log(j, component, decision, fields)
}
