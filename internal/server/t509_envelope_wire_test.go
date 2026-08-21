// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
)

func TestT509ChatWireAttachesValidEnvelope(t *testing.T) {
	text := envelope.Format(&envelope.Message{
		Kind:    envelope.KindFinishReport,
		Target:  "T509",
		SHA:     "abcdef0123456",
		Verdict: envelope.VerdictGreen,
		Payload: "Landed.",
	})
	line, ok := chatWireLine(claudia.Event{Type: "assistant", Text: text})
	if !ok {
		t.Fatal("wire dropped the envelope message")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	env, _ := m["envelope"].(map[string]any)
	if env == nil {
		t.Fatalf("missing envelope on wire: %s", line)
	}
	if env["kind"] != "finish-report" || env["target"] != "T509" {
		t.Fatalf("envelope=%v", env)
	}
	if _, bad := m["envelope_error"]; bad {
		t.Fatalf("valid envelope was flagged: %v", m["envelope_error"])
	}
}

func TestT509ChatWireFlagsMalformedLoadBearing(t *testing.T) {
	text := "```jevons\njevons: kind finish-report\njevons: target T509\n```\n\nDone."
	line, ok := chatWireLine(claudia.Event{Type: "assistant", Text: text})
	if !ok {
		t.Fatal("malformed envelope must still be delivered")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	if m["envelope_error"] == nil {
		t.Fatalf("expected envelope_error: %s", line)
	}
	msg, _ := m["message"].(map[string]any)
	content, _ := msg["content"].([]any)
	block, _ := content[0].(map[string]any)
	display, _ := block["text"].(string)
	if !strings.Contains(display, envelope.BannerHeading) {
		t.Fatalf("display missing banner:\n%s", display)
	}
	if !strings.Contains(display, "Done.") {
		t.Fatal("payload must still be on the wire")
	}
}

func TestT509ChatWireUnenvelopedUnchanged(t *testing.T) {
	line, ok := chatWireLine(claudia.Event{Type: "assistant", Text: "hello"})
	if !ok {
		t.Fatal("dropped")
	}
	if strings.Contains(line, `"envelope"`) {
		t.Fatalf("unenveloped message must not grow an envelope field: %s", line)
	}
}
