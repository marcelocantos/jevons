// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func t1120MuxFrame(t *testing.T, index int, typ, origin, text, stop string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id":    "e:" + strings.TrimPrefix(typ, "") + string(rune('0'+index)),
		"index": index,
		"op":    "put",
		"type":  typ,
		"event": map[string]any{
			"type":        typ,
			"turn_origin": origin,
			"message": map[string]any{
				"content":     []any{map[string]any{"type": "text", "text": text}},
				"stop_reason": stop,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := json.Marshal(map[string]any{"v": 1, "ch": ownerMuxChannel, "t": "frame", "body": json.RawMessage(body)})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func t1120MuxPhase(t *testing.T, phase string) []byte {
	t.Helper()
	env, err := json.Marshal(map[string]any{
		"v": 1, "ch": ownerMuxChannel, "t": "meta",
		"body": map[string]any{"phase": map[string]any{"phase": phase}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestT1120OneReplyThenQuietIdle(t *testing.T) {
	frames := make(chan []byte, 8)
	prompt := "Reply with exactly: TOKEN"
	frames <- t1120MuxFrame(t, 1, "user", "owner", prompt, "")
	frames <- t1120MuxPhase(t, "accepted")
	frames <- t1120MuxFrame(t, 2, "assistant", "", "TOKEN", "end_turn")
	frames <- t1120MuxPhase(t, "idle")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitOwnerMuxOneReplyThenQuiet(ctx, frames, prompt, "TOKEN", 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestT1120SecondBubbleFails(t *testing.T) {
	frames := make(chan []byte, 8)
	prompt := "Reply with exactly: TOKEN"
	frames <- t1120MuxFrame(t, 1, "user", "owner", prompt, "")
	frames <- t1120MuxFrame(t, 2, "assistant", "", "TOKEN", "end_turn")
	frames <- t1120MuxFrame(t, 3, "assistant", "", "Same status as my last summary", "end_turn")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := waitOwnerMuxOneReplyThenQuiet(ctx, frames, prompt, "TOKEN", time.Second)
	if err == nil || !strings.Contains(err.Error(), "second assistant bubble") {
		t.Fatalf("got %v, want second-bubble error", err)
	}
}

func TestT1120StuckReceivedFails(t *testing.T) {
	frames := make(chan []byte, 8)
	prompt := "Reply with exactly: TOKEN"
	frames <- t1120MuxFrame(t, 1, "user", "owner", prompt, "")
	frames <- t1120MuxFrame(t, 2, "assistant", "", "TOKEN", "end_turn")
	frames <- t1120MuxPhase(t, "accepted")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := waitOwnerMuxOneReplyThenQuiet(ctx, frames, prompt, "TOKEN", 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "never returned to idle") {
		t.Fatalf("got %v, want idle-missing error", err)
	}
}
