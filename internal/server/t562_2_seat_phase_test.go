// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/muxwin"
)

// 🎯T562.2: a worker/aside channel carries its OWN phase from the progress
// hub, live and in the window meta; the overseer's phase never leaks onto it.
func TestT562_2SeatPhaseIsPerSeatAndNeverTheOverseers(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	win := muxwin.Resolved{Lo: 1, Hi: 0, Following: true}

	// Overseer mid-turn; the worker has never been observed.
	s.beginOverseerPhase(nil)
	if _, ok := s.muxTranscriptMeta("jv-w", win, 0, false)["phase"]; ok {
		t.Fatal("unobserved seat must carry no phase (overseer's phase leaked)")
	}

	h := newMuxHub()
	sess := &muxSession{send: make(chan []byte, 8), transcripts: map[string]*muxWatch{"jv-w": {subscribed: true}}}
	h.add(sess)
	s.mux = h

	// A user turn reaches the worker: live meta says busy, window meta agrees.
	if !s.ObserveAgentProgress("jv-w", claudia.Event{Type: "user"}) {
		t.Fatal("first observation should change the summary")
	}
	var env muxEnvelope
	select {
	case raw := <-sess.send:
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("no live phase meta fanned to the seat's watcher")
	}
	var body struct {
		Phase PhaseSample `json:"phase"`
	}
	if err := json.Unmarshal(env.Body, &body); err != nil {
		t.Fatal(err)
	}
	if env.Ch != "transcript:jv-w" || env.T != "meta" || !body.Phase.Working() {
		t.Fatalf("live frame = %+v body=%+v", env, body)
	}
	p, ok := s.muxTranscriptMeta("jv-w", win, 0, false)["phase"].(PhaseSample)
	if !ok || !p.Working() {
		t.Fatalf("window meta phase = %#v ok=%v hub=%+v", p, ok, s.agentProgress.Get("jv-w"))
	}

	// The overseer going idle does not touch the worker's busy phase.
	s.setOverseerPhase(PhaseSample{Phase: PhaseIdle})
	if p, _ := s.muxTranscriptMeta("jv-w", win, 0, false)["phase"].(PhaseSample); !p.Working() {
		t.Fatalf("overseer idle flipped the worker: %#v", p)
	}

	// The worker's own turn ends: it reads idle, the overseer still busy.
	s.beginOverseerPhase(nil)
	s.agentProgress.SetStatus("jv-x", "running") // idle baseline seat
	if p, ok := s.muxTranscriptMeta("jv-x", win, 0, false)["phase"].(PhaseSample); !ok || p.Phase != PhaseIdle {
		t.Fatalf("idle seat under a busy overseer = %#v ok=%v", p, ok)
	}
}
