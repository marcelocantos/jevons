// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestT625ExactReplyRejectsUnrelatedEvidence(t *testing.T) {
	const token = "fresh-token"
	const prompt = "Reply with exactly: " + token
	frame := func(kind, origin string, content any, stop string) []byte {
		body, err := json.Marshal(map[string]any{
			"type": kind, "turn_origin": origin, "stream_id": "reply",
			"message": map[string]any{"content": content, "stop_reason": stop},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	blocks := func(text string) any { return []any{map[string]any{"type": "text", "text": text}} }
	stream := func(id, text, stop string) []byte {
		body, err := json.Marshal(map[string]any{
			"type": "assistant", "stream_id": id,
			"message": map[string]any{"content": blocks(text), "stop_reason": stop},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	user := frame("user", "owner", blocks(prompt), "")
	reply := frame("assistant", "", blocks(token), "end_turn")
	other := stream("other", "unrelated response", "end_turn")
	for _, tc := range []struct {
		name   string
		frames [][]byte
		wantOK bool
	}{
		{"typed owner", [][]byte{user, reply}, true},
		{"legacy owner", [][]byte{frame("user", "", prompt, ""), reply}, true},
		{"legacy assistant", [][]byte{user, frame("assistant", "", token, "end_turn")}, true},
		{"reply chunks", [][]byte{user, frame("assistant", "", blocks("fresh-"), ""), frame("assistant", "", blocks("token"), "end_turn")}, true},
		{"unrelated completed turn then requested reply", [][]byte{user, other, reply}, true},
		{"reply before owner echo", [][]byte{reply, user}, false},
		{"no owner echo", [][]byte{reply}, false},
		{"another exact prompt", [][]byte{frame("user", "owner", blocks("Reply with exactly: stale-token"), ""), reply}, false},
		{"agent echoes request", [][]byte{frame("user", "agent", blocks(prompt), ""), reply}, false},
		{"unsupported content carries token", [][]byte{frame("user", "owner", []any{map[string]any{"type": "image", "text": prompt}}, ""), reply}, false},
		{"unrelated answer", [][]byte{user, other}, false},
		{"generic journey answer", [][]byte{user, frame("assistant", "", blocks("journey succeeded"), "end_turn")}, false},
		{"token mentioned but not returned", [][]byte{user, frame("assistant", "", blocks("I cannot return "+token), "end_turn")}, false},
		{"no terminal", [][]byte{user, frame("assistant", "", blocks(token), "")}, false},
		{"truncated reply", [][]byte{user, frame("assistant", "", blocks(token), "max_tokens")}, false},
		{"token split across different turns", [][]byte{user, stream("a", "fresh-", "end_turn"), stream("b", "token", "end_turn")}, false},
		{"another stream ends the matching fragment", [][]byte{user, stream("a", token, ""), stream("b", "", "end_turn")}, false},
		{"interleaved fragments manufacture a token", [][]byte{user, stream("a", "fresh-", ""), stream("b", "token", "end_turn")}, false},
		{"interleaved unrelated terminal preserves requested stream", [][]byte{user, stream("a", "fresh-", ""), other, stream("a", "token", "end_turn")}, true},
		{"unrelated truncation then requested reply", [][]byte{user, stream("other", "unrelated", "max_tokens"), reply}, true},
		{"late amendment of completed stream", [][]byte{user, stream("reply", "", "end_turn"), reply}, false},
		{"missing stream identity", [][]byte{user, stream("", token, "end_turn")}, false},
		{"tool terminal alone", [][]byte{user, frame("assistant", "", []any{map[string]any{"type": "tool_use", "text": token}}, "end_turn")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := make(chan []byte, len(tc.frames))
			for _, f := range tc.frames {
				frames <- f
			}
			close(frames)
			if err := waitExactReply(context.Background(), frames, prompt, token); (err == nil) != tc.wantOK {
				t.Fatalf("exact reply: err=%v wantOK=%v", err, tc.wantOK)
			}
		})
	}
}

func TestT625RecordedHistoryRequiresActualExchange(t *testing.T) {
	const prompt, reply = "Reply with exactly: fresh-token", "fresh-token"
	user := []byte(`{"type":"user","turn_origin":"owner","message":{"content":"Reply with exactly: fresh-token"}}`)
	answer := []byte(`{"type":"assistant","stream_id":"s","message":{"content":"fresh-token","stop_reason":"end_turn"}}`)
	for _, tc := range []struct {
		name   string
		frames [][]byte
		wantOK bool
	}{
		{"empty but live connection", nil, false},
		{"seed owner without reply", [][]byte{user}, false},
		{"seed reply without owner", [][]byte{answer}, false},
		{"seed exchange", [][]byte{user, answer}, true},
		{"historical failure before seed", [][]byte{[]byte(`{"type":"error","error":"old timeout"}`), user, answer}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {

			if err := assertRecordedOwnerReply(tc.frames, prompt, reply); (err == nil) != tc.wantOK {
				t.Fatalf("recorded exchange err=%v wantOK=%v", err, tc.wantOK)
			}
		})
	}
}

func TestT625ExactReplyHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitExactReply(ctx, make(chan []byte), "prompt", "reply"); err == nil {
		t.Fatal("cancellation was not reported")
	}
}
