// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// muxRecorder is a mux connection that records what was written to it, from
// the goroutine the `send` case answers on. replayBuf is not safe to read
// while that goroutine writes.
type muxRecorder struct {
	mu     sync.Mutex
	frames []map[string]any
}

func (r *muxRecorder) Write(_ context.Context, _ websocket.MessageType, data []byte) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, m)
	return nil
}

// await returns the first frame of type t, or nil once ctx is done.
func (r *muxRecorder) await(ctx context.Context, t string) map[string]any {
	for ctx.Err() == nil {
		r.mu.Lock()
		for _, f := range r.frames {
			if f["t"] == t {
				r.mu.Unlock()
				return f
			}
		}
		r.mu.Unlock()
	}
	return nil
}

// 🎯T852: an owner message that the daemon answers as `queued` is journaled
// and fanned as the owner's own turn BEFORE the client is told it queued.
//
// J31's aside arm submits the interleaved follow-up over a raw /ws/mux socket
// while the seat holds a tool call open (🎯T813), and then waits on the
// packaged UI's own subscription for a `user` frame with turn_origin=owner.
// That is two sessions: the one that sent, and the one that watches. The mux
// `send` case only unfreezes the SENDER's watch, so if the echo rode the
// sender's connection the owner would never see it — and a queued ack with no
// echo is exactly the transcript J31 timed out against.
func TestT852BusySeatMuxSendEchoesOwnerBeforeQueuedAck(t *testing.T) {
	dir := t.TempDir()
	db, err := statedb.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New("test", dir)
	s.overseerName = "jevons"
	s.SetStateDB(db)

	const seat = "react-aside-t852"
	const payload = "ACK-t852-owner-interleave"

	// The browser: watches the seat's transcript and sends nothing.
	watcher := &muxSession{send: make(chan []byte, 64), transcripts: make(map[string]*muxWatch)}
	s.mux.add(watcher)
	s.handleMuxEnvelope(t.Context(), &muxRecorder{}, watcher, muxEnvelope{
		V: 1, Ch: transcriptChannel(seat), T: "open",
	})

	// The deliver seam, as main.go wires it: admission is journaled on the
	// way through, and a seat with a turn in flight comes back queued
	// (mcpserver.deliverByNameWith → deliverToSenderMode).
	var seam struct {
		sync.Mutex
		origin string
	}
	s.SetAgentSendOriginHook(func(name, text, origin string, mode delivery.Mode) (AgentSendOutcome, error) {
		seam.Lock()
		seam.origin = origin
		seam.Unlock()
		if err := s.RecordAgentRequest(name, text, origin); err != nil {
			return AgentSendOutcome{}, err
		}
		return AgentSendOutcome{Status: "queued", Mechanism: delivery.MechanismClientQueue}, nil
	})

	// The raw submit socket: no watch of its own on this channel.
	sender := &muxRecorder{}
	s.handleMuxEnvelope(t.Context(), sender, &muxSession{
		send: make(chan []byte, 8), transcripts: make(map[string]*muxWatch),
	}, muxEnvelope{
		V: 1, Ch: transcriptChannel(seat), T: "send",
		Body: json.RawMessage(`{"text":"` + payload + `","mode":"submit"}`),
	})

	ack := sender.await(t.Context(), "status")
	if ack == nil {
		t.Fatal("the raw mux send was never acknowledged")
	}
	body, _ := ack["body"].(map[string]any)
	if body["status"] != "queued" || body["mode"] != "submit" {
		t.Fatalf("ack=%v, want status=queued mode=submit", body)
	}

	// Drained AFTER the ack was observed: anything here was fanned before or
	// at the same time as the ack, which is what the owner echo has to be.
	if got := ownerEchoText(t, watcher, payload); got == "" {
		t.Fatalf("the watching session got no owner echo for %q by the time the client was told "+
			"the daemon had queued it; the owner is told the message is held and the transcript "+
			"stays silent until the turn ends", payload)
	}

	seam.Lock()
	gotOrigin := seam.origin
	seam.Unlock()
	if gotOrigin != sendOriginOwner {
		t.Fatalf("the mux send carried origin %q, want %q", gotOrigin, sendOriginOwner)
	}

	// And it is durable, not only fanned: a fresh window replays it.
	replay := &muxRecorder{}
	reader := &muxSession{send: make(chan []byte, 64), transcripts: make(map[string]*muxWatch)}
	s.mux.add(reader)
	if err := s.writeMuxWindow(t.Context(), replay, reader, seat, -30, 0, true); err != nil {
		t.Fatal(err)
	}
	replay.mu.Lock()
	defer replay.mu.Unlock()
	found := false
	for _, f := range replay.frames {
		if f["t"] != "frame" {
			continue
		}
		fb, _ := f["body"].(map[string]any)
		if ev, ok := fb["event"].(map[string]any); ok && userEchoMatches(ev, payload) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the queued owner turn was not journaled: replay carries no owner user-frame for %q", payload)
	}
}

// ownerEchoText drains sess and returns the text of the first `user` frame
// whose turn_origin is owner and which carries want.
func ownerEchoText(t *testing.T, sess *muxSession, want string) string {
	t.Helper()
	for len(sess.send) > 0 {
		var env map[string]any
		if err := json.Unmarshal(<-sess.send, &env); err != nil {
			t.Fatal(err)
		}
		if env["t"] != "frame" {
			continue
		}
		body, _ := env["body"].(map[string]any)
		ev, ok := body["event"].(map[string]any)
		if !ok {
			continue
		}
		if userEchoMatches(ev, want) {
			return want
		}
	}
	return ""
}

// userEchoMatches is J31's ownerEcho predicate (boundary-oracle.cjs): a user
// frame, spoken by the owner, carrying this text.
func userEchoMatches(ev map[string]any, want string) bool {
	if ev["type"] != "user" || ev["turn_origin"] != sendOriginOwner {
		return false
	}
	return strings.Contains(proseFromEvent(ev), want)
}
