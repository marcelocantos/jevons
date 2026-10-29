// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1218: a native provider tool_call_update in progress paints a KindSteps
// row on the live inspect path before the command finishes.
func TestT1218ActiveNativeToolUpdatePaintsStep(t *testing.T) {
	raw := []byte(`{"sessionId":"s1","update":{"sessionUpdate":"tool_call_update","toolCallId":"bash-1","title":"Bash: bin/gate -- make test-go","status":"in_progress","rawInput":{"command":"bin/gate -- make test-go"}}}`)
	line, ok := chatWireLine(claudia.Event{Type: "progress", ProgressType: "tool_use", Raw: raw})
	if !ok {
		t.Fatal("chatWireLine must paint an in-progress native tool")
	}
	if !strings.Contains(line, "Bash") || !strings.Contains(line, "execution_status") {
		t.Fatalf("line=%s want Bash + execution_status", line)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	content, _ := m["message"].(map[string]any)
	blocks, _ := content["content"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("content=%v want one tool_use block", content)
	}
	blk, _ := blocks[0].(map[string]any)
	if blk["type"] != "tool_use" || blk["execution_status"] != "in_progress" {
		t.Fatalf("block=%v want in_progress tool_use", blk)
	}
}

func TestT1218JournalLosslessNativeProgressFoldsToStep(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	const name = "jv-t1218-native"
	s.DeliverInspectLive(name, claudia.Event{Type: "user", Text: "go"})
	lossless := `{"type":"progress","recorded":"lossless","progress_type":"tool_use","timestamp":"2026-10-24T10:00:00Z","raw":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"bash-9","title":"Shell","status":"in_progress","rawInput":{"command":"sleep 2"}}}}`
	s.muxFanTranscript(name, lossless)
	evs := s.mux.eventsFor(name)
	var saw bool
	for _, ev := range evs {
		if ev.Type != "tool_use" {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(ev.Body, &m)
		if m["name"] == "Shell" && m["execution_status"] == "in_progress" {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatalf("mux events=%v want Shell in_progress tool_use", evs)
	}
}
