// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// chat-smoke-cancel: live cancel-and-send against a RUNNING jevonsd
// (defaults to daily :13705 — Universe A). Prefer make test-journey for
// routine cancel E2E; use this when you intentionally want the live session.
//
//	go run ./scripts/chat-smoke-cancel
//
// Protocol (must not false-pass on the cancelled turn's end_turn):
//  1. Start a long turn; wait for in-flight assistant activity.
//  2. Interrupt; wait for that turn's terminal (cancel settled).
//  3. Send the replacement; wait for its user echo.
//  4. Wait for a terminal AFTER the replacement user echo.
//  5. Fail on wire error frames.
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
	timeout := flag.Duration("timeout", 120*time.Second, "overall timeout")
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

	long := "Count slowly from 1 to 30, one number per line, with a short phrase each line."
	fmt.Println("send long:", long)
	if err := conn.Write(ctx, websocket.MessageText, []byte(long)); err != nil {
		fmt.Fprintf(os.Stderr, "write long: %v\n", err)
		os.Exit(1)
	}

	// Wait for in-flight assistant activity.
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
			if m["type"] == "error" {
				fmt.Fprintf(os.Stderr, "error before interrupt: %v\n", m["error"])
				os.Exit(1)
			}
			if m["type"] == "assistant" {
				fmt.Println("saw in-flight assistant frame; interrupting")
				break waitStream
			}
		case <-deadline:
			fmt.Fprintln(os.Stderr, "timeout waiting for long-turn stream")
			os.Exit(1)
		}
	}

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"interrupt"}`)); err != nil {
		fmt.Fprintf(os.Stderr, "interrupt: %v\n", err)
		os.Exit(1)
	}

	// Wait for the CANCELLED turn to terminal (or cancel_settled) BEFORE
	// sending the replacement — otherwise the cancelled end_turn is
	// misread as the replacement completing with empty text.
	cancelSettled := false
	deadlineCancel := time.After(30 * time.Second)
waitCancel:
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				fmt.Fprintln(os.Stderr, "conn closed waiting for cancel settle")
				os.Exit(1)
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			typ, _ := m["type"].(string)
			if typ == "error" {
				fmt.Fprintf(os.Stderr, "error during cancel: %v\n", m["error"])
				os.Exit(1)
			}
			if typ == "status" {
				if st, _ := m["state"].(string); st == "cancel_settled" || st == "idle" {
					cancelSettled = true
					fmt.Println("cancel settled via status:", st)
					break waitCancel
				}
			}
			if typ == "assistant" {
				msg, _ := m["message"].(map[string]any)
				stop, _ := msg["stop_reason"].(string)
				if stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens" {
					cancelSettled = true
					fmt.Println("cancel settled via end_turn")
					break waitCancel
				}
			}
		case <-deadlineCancel:
			fmt.Fprintln(os.Stderr, "timeout waiting for cancel to settle")
			os.Exit(1)
		}
	}
	if !cancelSettled {
		fmt.Fprintln(os.Stderr, "cancel did not settle")
		os.Exit(1)
	}

	replacement := "Reply with exactly the single word: CANCELLED-OK"
	fmt.Println("send correction:", replacement)
	if err := conn.Write(ctx, websocket.MessageText, []byte(replacement)); err != nil {
		fmt.Fprintf(os.Stderr, "write correction: %v\n", err)
		os.Exit(1)
	}

	gotUser := false
	var asst strings.Builder
	sawAsstFrame := false
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
					asst.Reset()
					fmt.Println("user echo:", s)
				}
			}
			// Only after correction user echo — never the cancelled turn.
			if typ == "assistant" && gotUser {
				sawAsstFrame = true
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
				if stop == "end_turn" || stop == "stop_sequence" || stop == "max_tokens" {
					terminal = true
				}
			}
		case <-deadline2:
			fmt.Fprintf(os.Stderr, "FAIL timeout; gotUser=%v asst=%q sawAsst=%v\n", gotUser, asst.String(), sawAsstFrame)
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
		fmt.Fprintln(os.Stderr, "FAIL: no terminal after correction user echo")
		os.Exit(1)
	}
	// Must have observed replacement-turn activity (text and/or tool frames
	// before end_turn), not merely a pre-correction cancel end_turn.
	if !sawAsstFrame {
		fmt.Fprintln(os.Stderr, "FAIL: no assistant frames after correction user echo")
		os.Exit(1)
	}
	fmt.Println("PASS cancel-and-send: cancel settled, then replacement user+terminal")
}
