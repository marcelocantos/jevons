// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/muxwin"
)

// 🎯T64.3: preamble prose fans as a put before tool_use; in-row token deltas
// fan as appends carrying text.
func TestT643MuxLiveFanPutThenToolsThenAppend(t *testing.T) {
	h := newMuxHub()
	watch := &muxWatch{
		subscribed: true,
		visible:    muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
		sub:        muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
		sent:       map[string]struct{}{},
	}
	sess := &muxSession{
		send:        make(chan []byte, 8),
		transcripts: map[string]*muxWatch{"jevons": watch},
	}
	h.add(sess)

	fan := func(line string) {
		folds, _ := h.applyLine("jevons", line)
		h.mu.Lock()
		h.fanFoldsLocked("jevons", folds)
		h.mu.Unlock()
	}

	fan(`{"type":"assistant","stream_id":"s1","message":{"content":[{"type":"text","text":"Checking."}]}}`)
	got := recvMuxFrame(t, sess)
	if got.Op != "put" || got.Type != "assistant" || !strings.Contains(got.Raw, "Checking.") {
		t.Fatalf("first token want assistant put with preamble, got %+v", got)
	}
	asstID := got.ID

	fan(`{"type":"assistant","stream_id":"s1","message":{"content":[{"type":"text","text":" more"}]}}`)
	got = recvMuxFrame(t, sess)
	if got.ID != asstID || got.Op != "append" || got.Text != " more" {
		t.Fatalf("in-row delta want append on %s, got %+v", asstID, got)
	}

	fan(`{"type":"assistant","stream_id":"s1","message":{"content":[{"type":"tool_use","name":"Read","id":"c1"}]}}`)
	got = recvMuxFrame(t, sess)
	if got.Op != "put" || got.Type != "tool_use" {
		t.Fatalf("tool row want put tool_use after preamble streamed, got %+v", got)
	}
}

// 🎯T64.3: a full send queue must not mark the put delivered; retry fans it.
func TestT643MuxDroppedPutEnqueueAllowsRetry(t *testing.T) {
	h := newMuxHub()
	watch := &muxWatch{
		subscribed: true,
		visible:    muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
		sub:        muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
		sent:       map[string]struct{}{},
	}
	sess := &muxSession{
		send:        make(chan []byte, 1),
		transcripts: map[string]*muxWatch{"jevons": watch},
	}
	h.add(sess)
	sess.send <- []byte("blocked")

	line := `{"type":"assistant","stream_id":"s1","message":{"content":[{"type":"text","text":"Checking."}]}}`
	putFold, _ := h.applyLine("jevons", line)
	if len(putFold) != 1 || putFold[0].Op != "put" {
		t.Fatalf("folds=%+v", putFold)
	}
	h.mu.Lock()
	h.fanFoldsLocked("jevons", putFold)
	h.mu.Unlock()
	if watch.alreadySent(putFold[0].Event.ID) {
		t.Fatal("dropped put must not mark the event sent")
	}

	appendFold, _ := h.applyLine("jevons", `{"type":"assistant","stream_id":"s1","message":{"content":[{"type":"text","text":" retry"}]}}`)
	h.mu.Lock()
	h.fanFoldsLocked("jevons", appendFold)
	h.mu.Unlock()
	if watch.alreadySent(putFold[0].Event.ID) {
		t.Fatal("append must not mark put delivered when put never enqueued")
	}

	<-sess.send
	h.mu.Lock()
	h.fanFoldsLocked("jevons", putFold)
	h.mu.Unlock()
	if !watch.alreadySent(putFold[0].Event.ID) {
		t.Fatal("successful put retry must mark sent")
	}
	got := recvMuxFrame(t, sess)
	if got.Op != "put" || !strings.Contains(got.Raw, "Checking.") {
		t.Fatalf("retry fan want put preamble, got %+v", got)
	}
}

type muxFrameGot struct {
	ID   string
	Op   string
	Type string
	Text string
	Raw  string
}

func recvMuxFrame(t *testing.T, sess *muxSession) muxFrameGot {
	t.Helper()
	select {
	case payload := <-sess.send:
		var env muxEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			t.Fatal(err)
		}
		var body struct {
			ID    string          `json:"id"`
			Op    string          `json:"op"`
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Event json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(env.Body, &body); err != nil {
			t.Fatal(err)
		}
		return muxFrameGot{
			ID: body.ID, Op: body.Op, Type: body.Type, Text: body.Text,
			Raw: string(body.Event),
		}
	default:
		t.Fatal("expected mux frame on session send channel")
		return muxFrameGot{}
	}
}
