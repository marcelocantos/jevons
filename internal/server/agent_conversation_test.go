// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/chatlog"
)

// 🎯T309.2: the three conversation operations — transcript, live, send —
// resolve by agent name for the overseer exactly as they do for a worker.
// These are the acceptance-3 hermetic oracles.

// overseerFamilyServer wires a server with an owner chat journal holding one
// complete owner turn plus an injected worker notification.
func overseerFamilyServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	s := New("test", dir)
	s.overseerName = "jevons"
	clog, err := chatlog.Open(filepath.Join(dir, "chatlog", "jevons.jsonl"))
	if err != nil {
		t.Fatalf("chatlog.Open: %v", err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	s.SetChatLog(clog)

	for _, line := range []string{
		`{"type":"user","timestamp":"2026-08-08T01:00:00Z","message":{"role":"user","content":"ship the widget"}}`,
		`{"type":"assistant","timestamp":"2026-08-08T01:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"assistant","timestamp":"2026-08-08T01:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"on it"}],"stop_reason":"end_turn"}}`,
		`{"type":"agent_note","timestamp":"2026-08-08T01:00:03Z","text":"jv-t309-worker: landed abc123"}`,
	} {
		if err := clog.Append(line); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	return s
}

func TestOverseerTranscriptThroughAgentFamily(t *testing.T) {
	s := overseerFamilyServer(t)

	payload, ok := s.buildAgentTranscriptPayload("jevons")
	if !ok {
		t.Fatal("overseer must be addressable by name")
	}
	if payload["empty"] == true {
		t.Fatalf("want real turns, got %v", payload)
	}
	turns, _ := payload["turns"].([]map[string]any)
	if len(turns) != 3 {
		t.Fatalf("want user+assistant+note rows, got %v", payload["turns"])
	}
	if turns[0]["role"] != "user" || turns[0]["text"] != "ship the widget" {
		t.Fatalf("user row=%v", turns[0])
	}
	// Same shape as a fleet transcript row: turn_number/role/text.
	if turns[0]["turn_number"] != 1 {
		t.Fatalf("turn_number=%v", turns[0]["turn_number"])
	}
	if turns[1]["role"] != "assistant" || turns[1]["text"] != "on it" {
		t.Fatalf("assistant row=%v (tool_use must not leak as prose)", turns[1])
	}
	events, _ := payload["events"].([]map[string]any)
	if len(events) < 3 {
		t.Fatalf("want wire events including tool_use, got %v", payload["events"])
	}
	var sawTool bool
	for _, ev := range events {
		if ev["type"] != "assistant" {
			continue
		}
		msg, _ := ev["message"].(map[string]any)
		raw, _ := msg["content"].([]any)
		for _, blk := range raw {
			m, _ := blk.(map[string]any)
			if m["type"] == "tool_use" && m["name"] == "Bash" {
				sawTool = true
			}
		}
	}
	if !sawTool {
		t.Fatalf("history events must carry tool_use for applyEventTape, got %v", events)
	}
	if turns[2]["role"] != "agent_note" {
		t.Fatalf("note row=%v", turns[2])
	}
}

// The HTTP member of the family serves the overseer like any other agent.
func TestOverseerTranscriptHTTPByName(t *testing.T) {
	s := overseerFamilyServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/jevons/transcript", nil)
	req.SetPathValue("name", "jevons")
	rec := httptest.NewRecorder()
	s.handleAgentTranscript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Name   string           `json:"name"`
		Source string           `json:"source"`
		Empty  bool             `json:"empty"`
		Turns  []map[string]any `json:"turns"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Name != "jevons" || body.Source != conversationSourceChatlog {
		t.Fatalf("body=%+v", body)
	}
	if body.Empty || len(body.Turns) == 0 {
		t.Fatalf("want overseer turns over HTTP, got %+v", body)
	}
}

// Live subscribe by name: an inspect subscriber on the overseer receives
// agent_transcript live frames, the same wire class fleet agents use.
func TestOverseerLiveSubscribeByName(t *testing.T) {
	s := overseerFamilyServer(t)
	ch := make(chan string, 8)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	s.BroadcastChat(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"live word"}]}}`)

	select {
	case line := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "assistant" || m["name"] != "jevons" {
			t.Fatalf("frame=%v", m)
		}
	default:
		t.Fatal("overseer listener got no live frame")
	}
}

// A non-subscriber must not be fanned (no cross-talk with fleet inspect).
func TestOverseerLiveNotFannedToOtherAgentSubscriber(t *testing.T) {
	s := overseerFamilyServer(t)
	ch := make(chan string, 4)
	s.setInspectSub(ch, "worker-a")

	s.BroadcastChat(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`)

	select {
	case line := <-ch:
		t.Fatalf("worker-a subscriber received overseer frame: %s", line)
	default:
	}
}

// Send by name reaches the overseer with owner-turn semantics: the clean owner
// bubble is journaled/broadcast and the delivered text carries userTurnPrefix,
// exactly as the /ws/chat wire does — so send is not an owner-wire exclusive.
func TestOverseerSendByNameOwnerOrigin(t *testing.T) {
	s := overseerFamilyServer(t)
	var delivered []string
	s.notifySender = func(text string) error {
		delivered = append(delivered, text)
		return nil
	}
	ch := make(chan string, 8)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	status, err := s.sendToNamedAgentAs("jevons", "do the thing", sendOriginOwner)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if status != "sent" {
		t.Fatalf("status=%q", status)
	}
	if len(delivered) != 1 || !strings.HasPrefix(delivered[0], userTurnPrefix) {
		t.Fatalf("delivered=%v, want userTurnPrefix marker", delivered)
	}
	if !strings.Contains(delivered[0], "do the thing") {
		t.Fatalf("delivered=%v", delivered)
	}

	select {
	case line := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		msg, _ := m["message"].(map[string]any)
		// 🎯T384: content is typed blocks, like every assistant turn.
		if m["type"] != "user" || userBlockText(msg["content"]) != "do the thing" {
			t.Fatalf("owner bubble=%v", m)
		}
	default:
		t.Fatal("owner bubble was not broadcast on send-by-name")
	}
}

// origin=agent is an injected notification: delivered unmarked, no owner bubble.
func TestOverseerSendByNameAgentOrigin(t *testing.T) {
	s := overseerFamilyServer(t)
	var delivered []string
	s.notifySender = func(text string) error {
		delivered = append(delivered, text)
		return nil
	}
	ch := make(chan string, 8)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	if _, err := s.sendToNamedAgentAs("jevons", "worker reply", sendOriginAgent); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(delivered) != 1 || strings.HasPrefix(delivered[0], userTurnPrefix) {
		t.Fatalf("delivered=%v, want unmarked notification", delivered)
	}
	select {
	case line := <-ch:
		t.Fatalf("agent-origin send must not paint an owner bubble: %s", line)
	default:
	}
}

// Root and another name share the same HTTP handlers (not a second product).
func TestNamedAgentsShareTranscriptAndSendHandlers(t *testing.T) {
	s := overseerFamilyServer(t)
	s.notifySender = func(string) error { return nil }

	for _, name := range []string{"jevons", "jevons-po"} {
		tr := httptest.NewRequest(http.MethodGet, "/api/agents/"+name+"/transcript", nil)
		tr.SetPathValue("name", name)
		trec := httptest.NewRecorder()
		s.handleAgentTranscript(trec, tr)
		if name == "jevons" && trec.Code != http.StatusOK {
			t.Fatalf("%s transcript status=%d body=%s", name, trec.Code, trec.Body.String())
		}
		// jevons-po may be 404 (not registered) — still the same handler.
		if trec.Code != http.StatusOK && trec.Code != http.StatusNotFound &&
			trec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s transcript unexpected status=%d body=%s", name, trec.Code, trec.Body.String())
		}

		sr := httptest.NewRequest(http.MethodPost, "/api/agents/"+name+"/send",
			strings.NewReader(`{"text":"ping"}`))
		sr.SetPathValue("name", name)
		srec := httptest.NewRecorder()
		s.handleAgentSend(srec, sr)
		if name == "jevons" && srec.Code != http.StatusOK {
			t.Fatalf("%s send status=%d body=%s", name, srec.Code, srec.Body.String())
		}
		if srec.Code != http.StatusOK && srec.Code != http.StatusNotFound && srec.Code != http.StatusBadGateway {
			t.Fatalf("%s send unexpected status=%d body=%s", name, srec.Code, srec.Body.String())
		}
	}
}

// The HTTP send member routes the overseer too (no 404 "not registered").
func TestOverseerSendHTTPByName(t *testing.T) {
	s := overseerFamilyServer(t)
	s.notifySender = func(string) error { return nil }

	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons/send",
		strings.NewReader(`{"text":"hello overseer"}`))
	req.SetPathValue("name", "jevons")
	rec := httptest.NewRecorder()
	s.handleAgentSend(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body agentSendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Name != "jevons" || body.Status != "sent" {
		t.Fatalf("body=%+v", body)
	}
}

// A configured non-default overseer name resolves the same way (🎯T44).
func TestOverseerFamilyHonoursConfiguredName(t *testing.T) {
	s := overseerFamilyServer(t)
	s.overseerName = "ceo"
	if !s.isOverseerAgent("CEO") {
		t.Fatal("configured overseer name must resolve case-insensitively")
	}
	if s.isOverseerAgent("jevons") {
		t.Fatal("default name must not shadow the configured one")
	}
}

// Journal projection details: prefixed ACP echoes are not double-counted as a
// separate owner turn, and frames outside the conversation are skipped.
func TestOverseerTurnsFromWireProjection(t *testing.T) {
	turns := overseerTurnsFromWire([]string{
		`{"type":"system","timestamp":"2026-08-08T01:00:00Z"}`,
		`{"type":"user","message":{"role":"user","content":"first"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"a"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"b"}]}}`,
		`{"type":"user","message":{"role":"user","content":"` + strings.TrimSuffix(userTurnPrefix, "\n") + `\nsecond"}}`,
		`not json`,
	})
	if len(turns) != 3 {
		t.Fatalf("turns=%v", turns)
	}
	if turns[1]["text"] != "a\nb" {
		t.Fatalf("assistant fragments must join: %v", turns[1])
	}
	if turns[2]["turn_number"] != 2 || turns[2]["text"] != "second" {
		t.Fatalf("second turn=%v (prefix must be stripped)", turns[2])
	}
}

// 🎯T309.3: the exported seam main wires into mcpserver.SetOverseerDeliver
// carries origin through to the same framing the by-name send produces, so a
// fleet-layer caller reaching the overseer is indistinguishable from an HTTP
// one. Origin values are the agentSendRequest wire strings.
func TestDeliverToOverseerAsCarriesOrigin(t *testing.T) {
	s := overseerFamilyServer(t)
	var delivered []string
	s.notifySender = func(text string) error {
		delivered = append(delivered, text)
		return nil
	}
	ch := make(chan string, 8)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	// Agent origin: unmarked injection, no owner bubble — worker reply shape.
	if err := s.DeliverToOverseerAs("worker reply", sendOriginAgent); err != nil {
		t.Fatalf("agent origin: %v", err)
	}
	if len(delivered) != 1 || strings.HasPrefix(delivered[0], userTurnPrefix) {
		t.Fatalf("delivered=%v, want unmarked notification", delivered)
	}
	select {
	case line := <-ch:
		t.Fatalf("agent-origin deliver must not paint an owner bubble: %s", line)
	default:
	}

	// Owner origin: owner marker + owner bubble.
	if err := s.DeliverToOverseerAs("owner words", sendOriginOwner); err != nil {
		t.Fatalf("owner origin: %v", err)
	}
	if len(delivered) != 2 || !strings.HasPrefix(delivered[1], userTurnPrefix) {
		t.Fatalf("delivered=%v, want userTurnPrefix marker", delivered)
	}
	select {
	case <-ch:
	default:
		t.Fatal("owner-origin deliver did not broadcast an owner bubble")
	}

	// An unknown origin must not silently become an unmarked injection:
	// default is owner, matching the HTTP handler's default.
	if err := s.DeliverToOverseerAs("unspecified", ""); err != nil {
		t.Fatalf("default origin: %v", err)
	}
	if len(delivered) != 3 || !strings.HasPrefix(delivered[2], userTurnPrefix) {
		t.Fatalf("delivered=%v, want owner default", delivered)
	}
}
