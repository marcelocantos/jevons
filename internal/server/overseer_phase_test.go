// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/muxwin"
)

// phaseCapture subscribes a listener and decodes every progress frame the
// server interleaves on the chat stream (🎯T555.1).
type phaseCapture struct {
	ch chan string
}

func newPhaseCapture(s *Server) *phaseCapture {
	c := &phaseCapture{ch: make(chan string, 64)}
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, c.ch)
	s.mu.Unlock()
	return c
}

// frames drains the captured lines, returning decoded progress frames only.
func (c *phaseCapture) frames(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for {
		select {
		case line := <-c.ch:
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("non-JSON chat line %q: %v", line, err)
			}
			if m["type"] == "progress" && m["phase"] != nil {
				out = append(out, m)
			}
		default:
			return out
		}
	}
}

func correspondents(m map[string]any) []string {
	raw, _ := m["correspondent"].([]any)
	var out []string
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

func TestT555_1OwnerDrainAcceptedNoCorrespondent(t *testing.T) {
	s := &Server{}
	s.notifySender = func(string) error { return nil }
	cap := newPhaseCapture(s)
	_ = s.SendToOverseer(userTurnPrefix + "hello")
	fr := cap.frames(t)
	if len(fr) != 1 || fr[0]["phase"] != PhaseAccepted {
		t.Fatalf("want one accepted frame, got %v", fr)
	}
	if _, has := fr[0]["correspondent"]; has {
		t.Fatalf("owner turn must carry no correspondent: %v", fr[0])
	}
	if fr[0]["working"] != true {
		t.Fatalf("accepted must be working=true: %v", fr[0])
	}
	if _, has := fr[0]["text"]; has {
		t.Fatalf("progress frame must carry no bubble body: %v", fr[0])
	}
}

func TestT555_1FleetDrainAcceptedWithCorrespondent(t *testing.T) {
	s := &Server{}
	s.notifySender = func(string) error { return nil }
	cap := newPhaseCapture(s)
	_ = s.SendToOverseer("[Agent jevons-po responded]\nreport")
	fr := cap.frames(t)
	if len(fr) != 1 || fr[0]["phase"] != PhaseAccepted {
		t.Fatalf("want one accepted frame, got %v", fr)
	}
	if got := correspondents(fr[0]); len(got) != 1 || got[0] != "jevons-po" {
		t.Fatalf("want correspondent [jevons-po], got %v", got)
	}
	// Later ACP progress inherits the correspondent; idle clears it.
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "hi"})
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	fr = cap.frames(t)
	var phases []string
	for _, f := range fr {
		phases = append(phases, f["phase"].(string))
	}
	if strings.Join(phases, ",") != PhaseStreaming+","+PhaseIdle {
		t.Fatalf("want streaming,idle got %v", phases)
	}
	if got := correspondents(fr[0]); len(got) != 1 || got[0] != "jevons-po" {
		t.Fatalf("streaming must inherit correspondent, got %v", got)
	}
	if _, has := fr[1]["correspondent"]; has || fr[1]["working"] != false {
		t.Fatalf("idle must clear correspondent and working: %v", fr[1])
	}
	if p := s.OverseerPhase(); p.Phase != PhaseIdle || p.Correspondent != nil {
		t.Fatalf("history_meta snapshot not idle: %+v", p)
	}
}

func TestT555_1QueuedFleetNoteIsNotCorrespondent(t *testing.T) {
	s := &Server{}
	busy := false
	s.notifySender = func(string) error {
		if busy {
			return fmt.Errorf("grok acp: prompt already in flight")
		}
		return nil
	}
	cap := newPhaseCapture(s)
	_ = s.SendToOverseer(userTurnPrefix + "owner speaks")
	busy = true
	_ = s.SendToOverseer("[Agent jevons-po responded]\nqueued behind owner")
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: "answering owner"})
	for _, f := range cap.frames(t) {
		if _, has := f["correspondent"]; has {
			t.Fatalf("queued fleet note leaked into owner turn: %v", f)
		}
	}
	if p := s.OverseerPhase(); p.Phase != PhaseStreaming || p.Correspondent != nil {
		t.Fatalf("owner in flight sample wrong: %+v", p)
	}
}

func TestT555_1ProgressFramesAreNotBubbles(t *testing.T) {
	line := phaseWireLine(PhaseSample{Phase: PhaseTool, Step: "Read"})
	var m map[string]any
	_ = json.Unmarshal([]byte(line), &m)
	for _, k := range []string{"text", "role", "content", "message"} {
		if _, has := m[k]; has {
			t.Fatalf("progress frame carries bubble field %q: %s", k, line)
		}
	}
	if m["phase"] != PhaseTool || m["step"] != "Read" || m["working"] != true {
		t.Fatalf("frame shape: %s", line)
	}
}

func TestT555_1MapperSharedWithHub(t *testing.T) {
	cases := []struct {
		ev   claudia.Event
		want string
	}{
		{claudia.Event{Type: "user"}, PhaseAccepted},
		{claudia.Event{Type: "assistant", Text: "x"}, PhaseStreaming},
		{claudia.Event{Type: "assistant", IsError: true}, PhaseError},
		{claudia.Event{Type: "assistant", StopReason: "end_turn"}, PhaseIdle},
		{claudia.Event{Type: "progress", ProgressType: progressTypeThought}, PhaseThinking},
		{claudia.Event{Type: "progress", ProgressType: progressTypePermission}, PhasePermission},
	}
	for _, c := range cases {
		p, ok := phaseFromEvent(c.ev)
		if !ok || p.Phase != c.want {
			t.Fatalf("event %+v: want %s got %+v ok=%v", c.ev, c.want, p, ok)
		}
		hp, _ := progressFromEvent(c.ev)
		if c.ev.Type == "progress" {
			continue // hub keeps its tool-title path for progress rows
		}
		if hp.Phase != hubPhase(p.Phase) {
			t.Fatalf("hub projection diverged for %s: %q vs %q", c.want, hp.Phase, hubPhase(p.Phase))
		}
	}
	if _, ok := phaseFromEvent(claudia.Event{Type: "system"}); ok {
		t.Fatal("system event must carry no phase")
	}
}

func TestT555_3MapperConsumesTypedProgressAndUsage(t *testing.T) {
	p, ok := phaseFromEvent(claudia.Event{Type: "progress", ProgressType: progressTypeThought})
	if !ok || p.Phase != PhaseThinking {
		t.Fatalf("thought: %+v ok=%v", p, ok)
	}
	p, ok = phaseFromEvent(claudia.Event{Type: "progress", ProgressType: progressTypePromptAccepted})
	if !ok || p.Phase != PhaseAccepted {
		t.Fatalf("prompt_accepted: %+v ok=%v", p, ok)
	}
	p, ok = phaseFromEvent(claudia.Event{Type: "progress", ProgressType: progressTypePermission})
	if !ok || p.Phase != PhasePermission {
		t.Fatalf("permission: %+v ok=%v", p, ok)
	}
	p, ok = phaseFromEvent(claudia.Event{
		Type: "assistant",
		Text: "hi",
		Usage: claudia.Usage{OutputTokens: 9},
	})
	if !ok || p.Phase != PhaseStreaming || p.Tokens != 9 {
		t.Fatalf("usage tokens: %+v ok=%v", p, ok)
	}
	// Promoted ToolTitle wins; do not scrape Raw when it is set (🎯T555.3).
	p, ok = phaseFromEvent(claudia.Event{
		Type:         "progress",
		ProgressType: progressTypeToolUse,
		ToolTitle:    "Read",
		Raw:          []byte(`{"update":{"sessionUpdate":"tool_call","title":"stale-raw"}}`),
	})
	if !ok || p.Phase != PhaseTool || p.Step != "Read" {
		t.Fatalf("ToolTitle consume: %+v ok=%v", p, ok)
	}
	// Generic MCP: tool title stays bare tool (🎯T64.2).
	p, ok = phaseFromEvent(claudia.Event{
		Type:         "progress",
		ProgressType: progressTypeToolUse,
		ToolTitle:    "MCP: tool",
	})
	if !ok || p.Phase != PhaseTool || p.Step != "" {
		t.Fatalf("generic tool title must stay bare tool: %+v ok=%v", p, ok)
	}
	p, ok = phaseFromEvent(claudia.Event{
		Type:         "progress",
		ProgressType: progressTypeToolUse,
		ToolStatus:   "completed",
		ToolTitle:    "Read",
	})
	if ok {
		t.Fatalf("terminal ToolStatus must not mint a phase: %+v", p)
	}
}

func TestT555_2MuxMetaAndFanCarryPhaseSample(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	meta := s.muxTranscriptMeta(muxwin.Resolved{Lo: 1, Hi: 0, Following: true}, 0, false)
	p, ok := meta["phase"].(PhaseSample)
	if !ok || p.Phase != PhaseIdle {
		t.Fatalf("reload snapshot phase = %#v", meta["phase"])
	}
	h := newMuxHub()
	sess := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{"jevons": {subscribed: true}}}
	h.add(sess)
	s.mux = h
	s.beginOverseerPhase([]string{"jevons-po"})
	select {
	case raw := <-sess.send:
		var env muxEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(env.Body, &body); err != nil {
			t.Fatal(err)
		}
		phase, _ := body["phase"].(map[string]any)
		if phase["phase"] != PhaseAccepted {
			t.Fatalf("live mux fan phase = %#v", body["phase"])
		}
		corr, _ := phase["correspondent"].([]any)
		if len(corr) != 1 || corr[0] != "jevons-po" {
			t.Fatalf("correspondent = %#v", phase["correspondent"])
		}
	default:
		t.Fatal("mux fan did not publish phase")
	}
}
