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

	// Let history/subscribe settle.
	time.Sleep(300 * time.Millisecond)
	fmt.Println("send:", *prompt)
	if err := conn.Write(ctx, websocket.MessageText, []byte(*prompt)); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}

	working := true
	var summary []string
	for working {
		_, data, err := conn.Read(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read: %v\nsummary: %v\n", err, summary)
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

		if shouldClear(typ, msg, text, stop) {
			working = false
		}
	}
	fmt.Println("PASS working cleared")
	fmt.Println("events:", strings.Join(summary, " | "))
}

func shouldClear(typ string, msg map[string]any, text, stop string) bool {
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
	terminal := stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens"
	return terminal || (text != "" && stop == "")
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
