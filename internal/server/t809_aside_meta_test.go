// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import "testing"

// 🎯T809: the window meta a client gets on `open` is per channel. The
// overseer's turn phase belongs to the overseer's transcript only. It used to
// be stamped on every channel, so a fresh aside's sidebar composer read the
// overseer's mid-turn phase as its own busy seat, client-queued the owner's
// first message, sent no frame, and left the aside transcript blank.
func TestT809AsideOpenMetaCarriesNoOverseerPhase(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	s.beginOverseerPhase([]string{"jevons-po"}) // overseer mid-turn

	open := func(name string) map[string]any {
		t.Helper()
		buf := &replayBuf{}
		env := muxEnvelope{V: 1, Ch: transcriptChannel(name), T: "open"}
		sess := &muxSession{send: make(chan []byte, 8), transcripts: map[string]*muxWatch{}}
		s.handleMuxEnvelope(t.Context(), buf, sess, env)
		for _, f := range buf.frames {
			if f["t"] == "meta" {
				body, _ := f["body"].(map[string]any)
				return body
			}
		}
		t.Fatalf("no meta frame on open of %q: %v", name, buf.frames)
		return nil
	}

	if _, ok := open("jevons")["phase"]; !ok {
		t.Fatal("the overseer's own open must still carry its phase")
	}
	aside := open("react-aside-t809")
	for _, k := range []string{"phase", "working", "owner_ux", "overseer_down"} {
		if v, ok := aside[k]; ok {
			t.Fatalf("aside open meta carries overseer field %q = %v", k, v)
		}
	}
}
