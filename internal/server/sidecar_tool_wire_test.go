// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"

	"github.com/marcelocantos/claudia"
)

// A sidecar seat publishes a tool call with its name in ToolTitle and its
// arguments as JSON in Text, and no ACP Raw. The chat wire dropped it, so no
// sidecar seat's tool calls reached the chat (journey J6c, 2026-10-05).
func TestChatWireCarriesSidecarToolCalls(t *testing.T) {
	line, ok := chatWireLine(claudia.Event{
		Type: "progress", ProgressType: "tool_use",
		ToolCallID: "fc_1", ToolTitle: "jevons_idea_capture",
		Text: `{"text":"journey-token","source":"mcp"}`,
	})
	if !ok {
		t.Fatal("sidecar tool call produced no chat line")
	}
	var rec struct {
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &rec); err != nil || len(rec.Message.Content) != 1 {
		t.Fatalf("line %s: %v", line, err)
	}
	tu := rec.Message.Content[0]
	input, _ := tu["input"].(map[string]any)
	if tu["type"] != "tool_use" || tu["name"] != "jevons_idea_capture" || tu["id"] != "fc_1" || input["text"] != "journey-token" {
		t.Fatalf("tool_use = %v", tu)
	}
	// A progress event with neither ACP Raw nor a tool title stays dropped.
	if _, ok := chatWireLine(claudia.Event{Type: "progress", ProgressType: "tool_use"}); ok {
		t.Fatal("an anonymous progress event produced a chat line")
	}
}
