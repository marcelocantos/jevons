// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"

	"github.com/marcelocantos/jevons/internal/muxwin"
)

func TestObserveMCPToolCallStampsOldestGeneric(t *testing.T) {
	s := &Server{mux: newMuxHub(), overseerName: "jevons", overseerOwnerTurn: true}
	s.mux.fanTranscript("jevons", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"MCP: tool"}]}}`)
	s.mux.fanTranscript("jevons", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"MCP: tool"}]}}`)
	s.ObserveMCPToolCall("jevonsmcp__jevons_agent_list", map[string]any{"query": "running"})
	s.ObserveMCPToolCall("jevons_plan_usage", nil)
	evs := s.mux.eventsFor("jevons")
	if len(evs) != 2 {
		t.Fatalf("events=%d", len(evs))
	}
	var a, b map[string]any
	_ = json.Unmarshal(evs[0].Body, &a)
	_ = json.Unmarshal(evs[1].Body, &b)
	if a["name"] != "jevons_agent_list" {
		t.Fatalf("oldest=%v want jevons_agent_list", a["name"])
	}
	if b["name"] != "jevons_plan_usage" {
		t.Fatalf("second=%v want jevons_plan_usage", b["name"])
	}
}

func TestObserveMCPToolCallQueuesUntilGenericArrives(t *testing.T) {
	s := &Server{mux: newMuxHub(), overseerName: "jevons", overseerStreamID: "s1"}
	s.ObserveMCPToolCall("jevons_agent_list", map[string]any{"query": "running"})
	if muxwin.HasGenericSteps(s.mux.eventsFor("jevons")) {
		t.Fatal("must not mint a step from a stamp alone")
	}
	s.muxFanTranscript("jevons", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"MCP: tool"}]}}`)
	evs := s.mux.eventsFor("jevons")
	if len(evs) != 1 {
		t.Fatalf("events=%d", len(evs))
	}
	var m map[string]any
	_ = json.Unmarshal(evs[0].Body, &m)
	if m["name"] != "jevons_agent_list" {
		t.Fatalf("queued stamp not consumed: %v", m["name"])
	}
}

func TestObserveMCPToolCallIgnoresIdleWorkerCalls(t *testing.T) {
	s := &Server{mux: newMuxHub(), overseerName: "jevons"}
	s.ObserveMCPToolCall("jevons_agent_list", nil)
	if len(s.mux.stamps["jevons"]) != 0 {
		t.Fatalf("idle worker MCP must not queue onto the overseer: %+v", s.mux.stamps)
	}
}

func TestCleanMCPToolName(t *testing.T) {
	if got := cleanMCPToolName("jevonsmcp__jevons_agent_list"); got != "jevons_agent_list" {
		t.Fatalf("got %q", got)
	}
	if got := cleanMCPToolName("jevons_plan_usage"); got != "jevons_plan_usage" {
		t.Fatalf("got %q", got)
	}
}

func TestChatToolStampLineFoldsOntoGeneric(t *testing.T) {
	line := chatToolStampLine("jevons_agent_list", map[string]any{"query": "running"})
	if line == "" {
		t.Fatal("empty stamp line")
	}
	evs := muxwin.EventsFromLines([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"MCP: tool"}]}}`,
		line,
	})
	var m map[string]any
	_ = json.Unmarshal(evs[0].Body, &m)
	if m["name"] != "jevons_agent_list" {
		t.Fatalf("hydrate name=%v body=%s", m["name"], evs[0].Body)
	}
}
