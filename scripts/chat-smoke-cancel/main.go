// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// chat-smoke-cancel: live cancel-and-send against a running jevonsd.
//
//	go run ./scripts/chat-smoke-cancel
//
// Starts a long turn, interrupts, sends a short replacement, asserts the
// replacement is delivered and the turn reaches terminal idle.
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
	timeout := flag.Duration("timeout", 90*time.Second, "overall timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	url := "ws://" + *host + "/ws/chat"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)

	frames := make(chan []byte, 512)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				close(frames)
				return
			}
			frames <- data
		}
	}()

	// Drain replay.
	replayed := 0
drain:
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				fmt.Fprintln(os.Stderr, "connection closed during drain")
				os.Exit(1)
			}
			replayed++
		case <-time.After(700 * time.Millisecond):
			break drain
		}
	}
	fmt.Println("drained replay frames:", replayed)

	// Long turn so we can interrupt mid-flight.
	long := "Count slowly from 1 to 30, one number per line, with a short phrase each line."
	fmt.Println("send long:", long)
	if err := conn.Write(ctx, websocket.MessageText, []byte(long)); err != nil {
		fmt.Fprintf(os.Stderr, "write long: %v\n", err)
		os.Exit(1)
	}

	// Wait for any assistant activity so we know the turn is in flight.
	sawStream := false
	deadline := time.After(25 * time.Second)
waitStream:
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				fmt.Fprintln(os.Stderr, "conn closed waiting for stream")
				os.Exit(1)
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			if m["type"] == "assistant" {
				sawStream = true
				break waitStream
			}
			if m["type"] == "error" {
				fmt.Fprintf(os.Stderr, "error before interrupt: %v\n", m["error"])
				os.Exit(1)
			}
		case <-deadline:
			fmt.Fprintln(os.Stderr, "timeout waiting for long-turn stream")
			os.Exit(1)
		}
	}
	fmt.Println("saw in-flight assistant frame; interrupt + correct")

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"interrupt"}`)); err != nil {
		fmt.Fprintf(os.Stderr, "interrupt: %v\n", err)
		os.Exit(1)
	}
	// Brief pause so cancel can settle before the replacement prompt
	// (server still serialises cancel-and-send; this reduces load).
	time.Sleep(400 * time.Millisecond)
	replacement := "Reply with exactly the single word: CANCELLED-OK"
	fmt.Println("send correction:", replacement)
	if err := conn.Write(ctx, websocket.MessageText, []byte(replacement)); err != nil {
		fmt.Fprintf(os.Stderr, "write correction: %v\n", err)
		os.Exit(1)
	}

	gotUser := false
	var asst strings.Builder
	terminal := false
	deadline2 := time.After(60 * time.Second)
	for !terminal {
		select {
		case data, ok := <-frames:
			if !ok {
				fmt.Fprintln(os.Stderr, "conn closed waiting for correction")
				os.Exit(1)
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			typ, _ := m["type"].(string)
			if typ == "error" {
				fmt.Fprintf(os.Stderr, "FAIL delivery error: %v\n", m["error"])
				os.Exit(1)
			}
			msg, _ := m["message"].(map[string]any)
			if typ == "user" {
				if s, ok := msg["content"].(string); ok && strings.Contains(s, "CANCELLED-OK") {
					gotUser = true
					asst.Reset() // ignore residual text from cancelled turn
					fmt.Println("user echo:", s)
				}
			}
			// Only count frames after the correction user echo so residual
			// stream from the cancelled turn is ignored.
			if typ == "assistant" && gotUser {
				stop, _ := msg["stop_reason"].(string)
				if content, ok := msg["content"].([]any); ok {
					for _, c := range content {
						cm, _ := c.(map[string]any)
						if cm["type"] == "text" {
							if t, _ := cm["text"].(string); t != "" {
								asst.WriteString(t)
							}
						}
					}
				}
				// Terminal after correction user = replacement turn done
				// (text and/or tools). Empty cancel end_turns before the
				// replacement starts are ignored by requiring gotUser first.
				if stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens" {
					terminal = true
				}
			}
		case <-deadline2:
			fmt.Fprintf(os.Stderr, "FAIL timeout; gotUser=%v asst=%q sawStream=%v\n", gotUser, asst.String(), sawStream)
			os.Exit(1)
		}
	}

	text := strings.TrimSpace(asst.String())
	fmt.Println("assistant text:", text)
	if !gotUser {
		fmt.Fprintln(os.Stderr, "FAIL: no user echo for correction")
		os.Exit(1)
	}
	if !terminal {
		fmt.Fprintln(os.Stderr, "FAIL: no terminal after correction")
		os.Exit(1)
	}
	fmt.Println("PASS cancel-and-send: replacement delivered, terminal reached")
	_ = sawStream
}
