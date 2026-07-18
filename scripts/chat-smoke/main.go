// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// chat-smoke: live headless /ws/chat round-trip against a running jevonsd.
//
//	go run ./scripts/chat-smoke -prompt 'Reply with exactly: pong'
//
// Exit 0 when a wire frame clears the working indicator (assistant text
// or terminal end_turn). Exit 1 on timeout / protocol failure.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	host := flag.String("host", "127.0.0.1:13705", "jevonsd host:port")
	prompt := flag.String("prompt", "Reply with exactly: pong", "chat prompt")
	timeout := flag.Duration("timeout", 90*time.Second, "overall timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	url := "ws://" + *host + "/ws/chat"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial %s: %v\n", url, err)
		os.Exit(1)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)

	// Drain the journal replay (🎯T49): every connect replays full
	// history, and asserting on replayed frames masks live failures
	// (the T30.1 drill PASSed on a stale bubble while the real turn
	// collided with an in-flight prompt). History arrives as a burst;
	// treat 700ms of silence as end-of-replay, then assert only on
	// frames after our own send. A dedicated reader goroutine feeds a
	// channel — cancelling a coder/websocket Read kills the whole
	// connection, so per-read timeouts are not an option.
	frames := make(chan []byte, 256)
	readErr := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				readErr <- err
				close(frames)
				return
			}
			frames <- data
		}
	}()
	replayed := 0
drain:
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				fmt.Fprintf(os.Stderr, "read during replay drain: %v\n", <-readErr)
				os.Exit(1)
			}
			replayed++
		case <-time.After(700 * time.Millisecond):
			break drain
		}
	}
	fmt.Println("drained replay frames:", replayed)
	fmt.Println("send:", *prompt)
	if err := conn.Write(ctx, websocket.MessageText, []byte(*prompt)); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}

	working := true
	var summary []string
	var bubbles []string
	open := -1
	asstChunks := 0
	for working {
		data, ok := <-frames
		if !ok {
			fmt.Fprintf(os.Stderr, "read: %v\nsummary: %v\n", <-readErr, summary)
			os.Exit(1)
		}
		var m map[string]any
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		typ, _ := m["type"].(string)
		if typ == "" {
			fmt.Println("untyped:", trim(string(data), 100))
			continue
		}
		msg, _ := m["message"].(map[string]any)
		stop, _ := msg["stop_reason"].(string)
		text := assistantText(msg)
		if typ == "user" {
			if s, ok := msg["content"].(string); ok {
				text = s
			}
		}
		line := fmt.Sprintf("%s stop=%q text=%q", typ, stop, trim(text, 60))
		summary = append(summary, line)
		fmt.Println("event:", line)

		if typ == "assistant" && text != "" {
			asstChunks++
			if open >= 0 {
				bubbles[open] += text
			} else {
				bubbles = append(bubbles, text)
				open = len(bubbles) - 1
			}
			// Mid-stream must not clear.
			if shouldClear(typ, msg, stop) {
				fmt.Fprintf(os.Stderr, "FAIL: mid-stream clear on chunk %q\n", text)
				os.Exit(1)
			}
		}
		if shouldClear(typ, msg, stop) {
			working = false
			open = -1
		}
	}
	if len(bubbles) != 1 {
		fmt.Fprintf(os.Stderr, "FAIL: coalesce produced %d bubbles %q (chunks=%d); want 1\n",
			len(bubbles), bubbles, asstChunks)
		os.Exit(1)
	}
	if strings.TrimSpace(bubbles[0]) == "" {
		fmt.Fprintf(os.Stderr, "FAIL: empty assistant bubble\n")
		os.Exit(1)
	}
	// Multi-token replies must not arrive as one wire frame only when the
	// model streams — but a single-frame reply is still one bubble (ok).
	fmt.Println("PASS working cleared; one assistant bubble:", trim(bubbles[0], 80))
	fmt.Println("chunks:", asstChunks, "events:", strings.Join(summary, " | "))
}

func shouldClear(typ string, msg map[string]any, stop string) bool {
	if typ == "system" {
		return true
	}
	if typ != "assistant" {
		return false
	}
	if msg == nil {
		return false
	}
	if _, ok := msg["content"].([]any); !ok {
		return false
	}
	return stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens"
}

func assistantText(msg map[string]any) string {
	if msg == nil {
		return ""
	}
	content, ok := msg["content"].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, c := range content {
		cm, _ := c.(map[string]any)
		if cm["type"] == "text" {
			if t, _ := cm["text"].(string); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "")
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
