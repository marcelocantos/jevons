// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func ownerMuxFixture(t *testing.T, channel, kind string, index int, text, stop, origin, op string) []byte {
	t.Helper()
	body := map[string]any{
		"v": 1, "ch": channel, "t": "frame",
		"body": map[string]any{
			"id": fmt.Sprintf("e:%d", index), "index": index, "op": op, "type": kind,
			"event": map[string]any{
				"type": kind, "turn_origin": origin,
				"message": map[string]any{
					"content":     []any{map[string]string{"type": "text", "text": text}},
					"stop_reason": stop,
				},
			},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestT625MuxReplyRequiresCompleteOwnSnapshot(t *testing.T) {
	const prompt, token = "Reply with exactly: fresh-token", "fresh-token"
	owner := ownerMuxFixture(t, ownerMuxChannel, "user", 2, prompt, "", "owner", "put")
	reply := func(index int, text, stop string) []byte {
		return ownerMuxFixture(t, ownerMuxChannel, "assistant", index, text, stop, "", "append")
	}
	for _, tc := range []struct {
		name   string
		frames [][]byte
		wantOK bool
	}{
		{"full terminal snapshot", [][]byte{owner, reply(3, token, "end_turn")}, true},
		{"repeated full snapshots replace rather than concatenate", [][]byte{owner, reply(3, "fresh-", ""), reply(3, token, ""), reply(3, token, "end_turn")}, true},
		{"unrelated terminal does not finish matching fragment", [][]byte{owner, reply(3, token, ""), reply(4, "", "end_turn")}, false},
		{"partial snapshots cannot manufacture token", [][]byte{owner, reply(3, "fresh-", ""), reply(3, "token", "end_turn")}, false},
		{"unrelated terminal then requested terminal", [][]byte{owner, reply(3, "other", "end_turn"), reply(4, token, "end_turn")}, true},
		{"truncation", [][]byte{owner, reply(3, token, "max_tokens")}, false},
		{"unrelated truncation", [][]byte{owner, reply(3, "other", "max_tokens"), reply(4, token, "end_turn")}, true},
		{"stale row updated after owner", [][]byte{owner, reply(1, token, "end_turn")}, false},
		{"reply precedes echo", [][]byte{reply(3, token, "end_turn"), owner, reply(3, token, "end_turn")}, false},
		{"terminal index rewritten under new ID", [][]byte{owner, reply(3, "other", "end_turn"), bytes.ReplaceAll(reply(3, token, "end_turn"), []byte(`"id":"e:3"`), []byte(`"id":"replacement"`))}, false},
		{"terminal row rewritten", [][]byte{owner, reply(3, "other", "end_turn"), reply(3, token, "end_turn")}, false},
		{"no echo", [][]byte{reply(3, token, "end_turn")}, false},
		{"agent-origin echo", [][]byte{ownerMuxFixture(t, ownerMuxChannel, "user", 2, prompt, "", "agent", "put"), reply(3, token, "end_turn")}, false},
		{"unknown-origin echo", [][]byte{ownerMuxFixture(t, ownerMuxChannel, "user", 2, prompt, "", "unknown", "put"), reply(3, token, "end_turn")}, false},
		{"wrong conversation", [][]byte{owner, ownerMuxFixture(t, "transcript:other", "assistant", 3, token, "end_turn", "", "put")}, false},
		{"merely mentions requested token", [][]byte{owner, reply(3, "I cannot return "+token, "end_turn")}, false},
		{"reset loses correlation", [][]byte{owner, []byte(`{"v":1,"ch":"transcript:jevons","t":"reset"}`), reply(3, token, "end_turn")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := assertOwnerMuxReplay(tc.frames, prompt, token); (err == nil) != tc.wantOK {
				t.Fatalf("err=%v wantOK=%v", err, tc.wantOK)
			}
		})
	}
}

func TestT625MuxReplayEndsAtMetaAndRequiresSeed(t *testing.T) {
	const prompt, token = "Reply with exactly: fresh-token", "fresh-token"
	owner := ownerMuxFixture(t, ownerMuxChannel, "user", 1, prompt, "", "owner", "put")
	reply := ownerMuxFixture(t, ownerMuxChannel, "assistant", 2, token, "end_turn", "", "put")
	status := []byte(`{"v":1,"ch":"transcript:jevons","t":"meta","body":{"working":true}}`)
	meta := []byte(`{"v":1,"ch":"transcript:jevons","t":"meta","body":{"n":2,"lo":1,"hi":0,"following":true}}`)
	for _, tc := range []struct {
		name   string
		frames [][]byte
		wantOK bool
	}{
		{"status before replay", [][]byte{status, owner, status, reply, meta}, true},
		{"status is not a replay boundary", [][]byte{owner, reply, status}, false},
		{"actual seed before meta", [][]byte{owner, reply, meta}, true},
		{"seed only arrives after replay", [][]byte{meta, owner, reply}, false},
		{"empty replay", [][]byte{meta}, false},
		{"no replay boundary", [][]byte{owner, reply}, false},
		{"owner without seed reply", [][]byte{owner, meta}, false},
		{"reply without owner", [][]byte{reply, meta}, false},
		{"other channel cannot seal replay", [][]byte{owner, reply, []byte(`{"v":1,"ch":"transcript:other","t":"meta"}`)}, false},
		{"unsupported version", [][]byte{owner, reply, []byte(`{"v":2,"ch":"transcript:jevons","t":"meta"}`)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := make(chan []byte, len(tc.frames))
			for _, frame := range tc.frames {
				frames <- frame
			}
			close(frames)
			replay, err := collectOwnerMuxReplay(context.Background(), frames)
			if err == nil {
				err = assertOwnerMuxReplay(replay, prompt, token)
			}
			if (err == nil) != tc.wantOK {
				t.Fatalf("err=%v wantOK=%v", err, tc.wantOK)
			}
		})
	}
	frames := make(chan []byte, maxReplayFrames+2)
	for range maxReplayFrames + 1 {
		frames <- owner
	}
	frames <- meta
	close(frames)
	if _, err := collectOwnerMuxReplay(context.Background(), frames); err == nil {
		t.Fatal("unbounded replay accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := collectOwnerMuxReplay(ctx, make(chan []byte)); err == nil {
		t.Fatal("replay cancellation ignored")
	}
	if err := waitOwnerMuxReply(ctx, make(chan []byte), prompt, token); err == nil {
		t.Fatal("reply cancellation ignored")
	}
}
