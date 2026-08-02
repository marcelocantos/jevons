// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestAgentProgressObserveToolCall(t *testing.T) {
	h := NewAgentProgressHub()
	raw, _ := json.Marshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": "tool_call",
			"title":         "Bash: go test ./...",
		},
	})
	changed := h.Observe("jv-t118", claudia.Event{
		Type:         "progress",
		ProgressType: "tool_use",
		Raw:          raw,
	})
	if !changed {
		t.Fatal("expected change on first tool_call")
	}
	got := h.Get("jv-t118")
	if got.Phase != "working" {
		t.Fatalf("phase=%q", got.Phase)
	}
	if got.Step == "" || !strings.Contains(got.Step, "Bash") {
		t.Fatalf("step=%q", got.Step)
	}
	if got.Summary == "" || !strings.Contains(got.Summary, "working") {
		t.Fatalf("summary=%q", got.Summary)
	}
	// Duplicate same frame → no change.
	if h.Observe("jv-t118", claudia.Event{Type: "progress", ProgressType: "tool_use", Raw: raw}) {
		t.Fatal("duplicate tool_call should not report change")
	}
}

func TestAgentProgressSkipsToolCallUpdate(t *testing.T) {
	h := NewAgentProgressHub()
	raw, _ := json.Marshal(map[string]any{
		"update": map[string]any{
			"sessionUpdate": "tool_call_update",
			"title":         "Bash: ls",
		},
	})
	if h.Observe("w", claudia.Event{Type: "progress", ProgressType: "tool_use", Raw: raw}) {
		t.Fatal("tool_call_update must not set progress")
	}
}

func TestAgentProgressTerminalStop(t *testing.T) {
	h := NewAgentProgressHub()
	_ = h.Observe("w", claudia.Event{
		Type:         "progress",
		ProgressType: "tool_use",
		Raw:          []byte(`{"update":{"sessionUpdate":"tool_call","title":"Read"}}`),
	})
	ev := claudia.Event{Type: "assistant", StopReason: "end_turn"}
	if !ev.IsTerminalStop() {
		t.Fatal("end_turn assistant must be terminal")
	}
	if !h.Observe("w", ev) {
		t.Fatal("terminal stop should change summary")
	}
	got := h.Get("w")
	if got.Phase != "idle" {
		t.Fatalf("after terminal phase=%q summary=%q", got.Phase, got.Summary)
	}
	if got.Summary != "idle" {
		t.Fatalf("summary=%q want idle", got.Summary)
	}
}

func TestAgentProgressSetStatusBaseline(t *testing.T) {
	h := NewAgentProgressHub()
	h.SetStatus("a", "running")
	if h.Get("a").Summary != "running" {
		t.Fatalf("baseline=%q", h.Get("a").Summary)
	}
	// Rich snapshot not clobbered by running baseline.
	_ = h.Observe("a", claudia.Event{
		Type:         "progress",
		ProgressType: "tool_use",
		Raw:          []byte(`{"update":{"sessionUpdate":"tool_call","title":"Grep"}}`),
	})
	h.SetStatus("a", "running")
	got := h.Get("a")
	if got.Step != "Grep" && !strings.Contains(got.Summary, "Grep") {
		t.Fatalf("rich progress clobbered: %+v", got)
	}
	h.SetStatus("a", "stopped")
	if h.Get("a").Phase != "idle" {
		t.Fatalf("stopped should idle phase: %+v", h.Get("a"))
	}
}

func TestComposeProgressSummaryTruncates(t *testing.T) {
	s := composeProgressSummary("working", strings.Repeat("x", 80))
	if n := len([]rune(s)); n > 48 {
		t.Fatalf("runes=%d s=%q", n, s)
	}
	if !strings.HasSuffix(s, "…") {
		t.Fatalf("want ellipsis, got %q", s)
	}
}
