// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"
)

func TestParseTranscriptChannel(t *testing.T) {
	name, ok := parseTranscriptChannel("transcript:jevons-po")
	if !ok || name != "jevons-po" {
		t.Fatalf("got %q ok=%v", name, ok)
	}
	if _, ok := parseTranscriptChannel("fleet"); ok {
		t.Fatal("fleet is not a transcript channel")
	}
	if _, ok := parseTranscriptChannel("transcript:"); ok {
		t.Fatal("empty name")
	}
}

func TestEncodeMuxEnvelope(t *testing.T) {
	b, err := encodeMux("transcript:jevons", "meta", map[string]any{"older": 0, "total": 3})
	if err != nil {
		t.Fatal(err)
	}
	var env muxEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	if env.V != 1 || env.Ch != "transcript:jevons" || env.T != "meta" {
		t.Fatalf("env=%+v", env)
	}
}

func TestMuxPageBodyEmptyClearsOlder(t *testing.T) {
	got := muxPageBody(12, 40, nil)
	if got["older"] != 0 || got["start"] != 0 {
		t.Fatalf("empty page must stop paging: %+v", got)
	}
	lines := []json.RawMessage{json.RawMessage(`{"type":"user"}`)}
	got = muxPageBody(10, 40, lines)
	if got["older"] != 10 || got["start"] != 10 {
		t.Fatalf("non-empty page must publish start/older: %+v", got)
	}
}

func TestMuxHubFansOnlyWatchers(t *testing.T) {
	h := newMuxHub()
	a := &muxSession{send: make(chan []byte, 4), transcripts: map[string]struct{}{"jevons": {}}}
	b := &muxSession{send: make(chan []byte, 4), transcripts: map[string]struct{}{"jevons-po": {}}}
	h.add(a)
	h.add(b)
	h.fanTranscript("jevons", `{"type":"user","message":{"content":"hi"}}`)
	select {
	case got := <-a.send:
		var env muxEnvelope
		if err := json.Unmarshal(got, &env); err != nil {
			t.Fatal(err)
		}
		if env.T != "frame" || env.Ch != "transcript:jevons" {
			t.Fatalf("a got %+v", env)
		}
	default:
		t.Fatal("watcher a got nothing")
	}
	select {
	case <-b.send:
		t.Fatal("po watcher must not get overseer live")
	default:
	}
}

func TestWriteMuxReplayEmptyThenMeta(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	frames := inspectReplay(t, s, "jevons")
	if len(frames) < 1 {
		t.Fatal("legacy inspect still hydrates for old UI")
	}
	// Mux replay of an empty journal is just meta — no conversation_reset.
	buf := &replayBuf{}
	if err := s.writeMuxReplay(t.Context(), buf, "jevons"); err != nil {
		t.Fatal(err)
	}
	if len(buf.frames) == 0 {
		t.Fatal("want meta")
	}
	last := buf.frames[len(buf.frames)-1]
	if last["t"] != "meta" {
		t.Fatalf("last=%v want t=meta", last)
	}
	if last["ch"] != "transcript:jevons" {
		t.Fatalf("ch=%v", last["ch"])
	}
	for _, m := range buf.frames {
		if m["t"] == "frame" && m["type"] == "conversation_reset" {
			t.Fatalf("mux must not send inspect conversation_reset: %v", m)
		}
	}
}
