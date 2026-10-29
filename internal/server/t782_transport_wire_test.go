// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

func TestT782TransportTurnDeathIsSystemRowNotAssistant(t *testing.T) {
	t.Parallel()
	specimens := []string{
		"Error: RetriableError: Connection stalled",
		"[canceled] http/2 stream closed",
	}
	acpChunk := []byte(`{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"x"}}}`)
	for _, msg := range specimens {
		ev := claudia.Event{
			Type:       "assistant",
			Text:       msg,
			StopReason: "end_turn",
		}
		line, ok := chatWireLine(ev)
		if !ok {
			t.Fatalf("%q: expected wire line", msg)
		}
		var probe map[string]any
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatalf("json: %v\n%s", err, line)
		}
		if probe["type"] != "provider_transport" {
			t.Fatalf("type=%v want provider_transport\n%s", probe["type"], line)
		}
		if probe["retryable"] != true {
			t.Fatalf("retryable=%v", probe["retryable"])
		}
		if fc, _ := probe["failure_class"].(string); fc != "backend_unavailable" {
			t.Fatalf("failure_class=%q", fc)
		}
	}
	quoted := claudia.Event{
		Type:       "assistant",
		Text:       "Error: RetriableError: Connection stalled",
		StopReason: "end_turn",
		Raw:        acpChunk,
	}
	if line, ok := chatWireLine(quoted); ok {
		var probe map[string]any
		_ = json.Unmarshal([]byte(line), &probe)
		if probe["type"] == "provider_transport" {
			t.Fatal("ACP chunk with the same words must stay authored, not provider_transport")
		}
	}
}

func TestT782TransportTurnDeathClassifiesTransientBackend(t *testing.T) {
	t.Parallel()
	ev := claudia.Event{
		Type:       "assistant",
		Text:       "Error: RetriableError: Connection stalled",
		StopReason: "end_turn",
	}
	class := classifyAssistantEvent(ev)
	if class != agenterr.ClassBackendUnavailable {
		t.Fatalf("class=%s", class)
	}
	if !class.IsTransient() {
		t.Fatal("transport turn death must be transient for fleet retry")
	}
	if !strings.Contains(agenterr.OwnerCopy(class, ev.Text), "backend_unavailable") {
		t.Fatalf("owner copy=%q", agenterr.OwnerCopy(class, ev.Text))
	}
}
