// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

func TestT862RetryExhaustionOnOwnerWire(t *testing.T) {
	for _, raw := range []string{
		"retry_exhausted: connection timed out after 1 retry",
		"retries exceeded: 503 Service Unavailable after 1 retry",
		"max retries exceeded: connection timed out after 1 retry",
	} {
		t.Run(raw, func(t *testing.T) {
			for _, transport := range []bool{true, false} {
				text := raw
				if !transport {
					text = wireOwnerReplyFault(raw)
				}
				ev := claudia.Event{Type: "assistant", Text: text, IsError: transport, StopReason: "end_turn"}
				if transport {
					ev.Reason = agenterr.CauseRetryExhausted
				}
				bare := ev
				bare.Reason = ""
				if line, ok := chatWireLine(bare); !ok {
					t.Fatal("terminal outcome was dropped")
				} else if strings.Contains(line, `"failure_class"`) {
					t.Fatalf("a sentence classified without a cause: %s", line)
				}
				line, ok := chatWireLine(ev)
				if !ok {
					t.Fatal("terminal outcome was dropped")
				}
				var wire struct {
					FailureClass string `json:"failure_class"`
					Message      struct {
						StopReason string `json:"stop_reason"`
						Content    []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(line), &wire); err != nil {
					t.Fatal(err)
				}
				if wire.Message.StopReason != "end_turn" || len(wire.Message.Content) != 1 {
					t.Fatalf("lost terminal outcome: %s", line)
				}
				gotText := wire.Message.Content[0].Text
				if transport {
					if wire.FailureClass != "retry_exhausted" || !strings.Contains(gotText, "after 1 retry") {
						t.Fatalf("lost exhaustion class or reported budget: %s", line)
					}
				} else if wire.FailureClass != "" || gotText != raw {
					t.Fatalf("authored report became a provider failure: %s", line)
				}
			}
		})
	}
}
