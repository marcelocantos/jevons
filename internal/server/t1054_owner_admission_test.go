// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/ownercomms"
	"testing"
)

// A classified NONE must produce no assistant body on the actual broadcast
// path, even though the original candidate draft had an acknowledgment.
func TestT1054RoutineNoneDoesNotPaintAcknowledgment(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 32)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.waiting = true
	s.mu.Unlock()
	send := func(body string) {
		s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: body})
		s.DeliverOverseerEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	}
	send(ownercomms.Response(ownercomms.Classify(ownercomms.Evidence{}), "No action needed; worker completed."))
	send(ownercomms.Response(ownercomms.Classify(ownercomms.Evidence{DirectOwnerQuestion: true}), "The requested answer."))
	close(ch)
	var bodies []string
	for line := range ch {
		var m struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		if m.Type == "assistant" {
			for _, c := range m.Message.Content {
				if c.Text != "" {
					bodies = append(bodies, c.Text)
				}
			}
		}
	}
	if len(bodies) != 1 || bodies[0] != "The requested answer." {
		t.Fatalf("owner-visible bodies: %q", bodies)
	}
}
