// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 🎯T806: an owner message the daemon cannot deliver must not stay displayed
// as sent. A non-busy refusal (broker not_owner) surfaces once as a
// send_error diagnostic naming the reason; the message stays queued; a later
// successful delivery announces itself exactly once and delivers the text once.
func TestT806OwnerRefusalIsUndeliveredThenDeliveredOnce(t *testing.T) {
	s := &Server{}
	s.notifyRetryDelay = time.Hour // retry is driven by hand here
	lines := make(chan string, 16)
	s.chatListeners = append(s.chatListeners, lines)

	const refusal = `broker protocol: not_owner (name="jevons"): grant jevons is not owned by this connection`
	refuse := true
	var delivered []string
	s.notifySender = func(text string) error {
		if refuse {
			return fmt.Errorf("%s", refusal)
		}
		delivered = append(delivered, text)
		return nil
	}

	drainFrames := func() []map[string]any {
		var out []map[string]any
		for {
			select {
			case l := <-lines:
				var m map[string]any
				if json.Unmarshal([]byte(l), &m) == nil {
					out = append(out, m)
				}
			default:
				return out
			}
		}
	}
	sendErrors := func(fs []map[string]any) []string {
		var out []string
		for _, f := range fs {
			if f["type"] == "send_error" {
				out = append(out, fmt.Sprint(f["text"]))
			}
		}
		return out
	}

	_ = s.SendToOverseer(userTurnPrefix + "decisions")
	first := sendErrors(drainFrames())
	if len(first) != 1 || !strings.Contains(first[0], "not_owner") || !strings.Contains(first[0], "not delivered") {
		t.Fatalf("want one undelivered diagnostic naming the reason, got %q", first)
	}
	if len(delivered) != 0 {
		t.Fatalf("delivered despite refusal: %v", delivered)
	}

	// Still refused on a retry: the owner is told once, not once per attempt.
	s.drainOverseerNotes()
	if again := sendErrors(drainFrames()); len(again) != 0 {
		t.Fatalf("repeat refusal re-announced: %q", again)
	}
	if !queueHasOwner(s.notifyQueue) {
		t.Fatal("refused owner message left the queue: not retrievable")
	}

	refuse = false
	s.drainOverseerNotes()
	ok := sendErrors(drainFrames())
	if len(ok) != 1 || !strings.Contains(ok[0], "delivered") || strings.Contains(ok[0], "not delivered") {
		t.Fatalf("want exactly one delivered announcement, got %q", ok)
	}
	if len(delivered) != 1 || !strings.Contains(delivered[0], "decisions") {
		t.Fatalf("want the message delivered once, got %v", delivered)
	}
	s.drainOverseerNotes()
	if extra := sendErrors(drainFrames()); len(extra) != 0 || len(delivered) != 1 {
		t.Fatalf("duplicate delivery/announcement: %q %v", extra, delivered)
	}
}

// A plain busy defer is normal queueing behind a running turn and stays quiet.
func TestT806BusyDeferStaysQuiet(t *testing.T) {
	s := &Server{}
	s.notifyRetryDelay = time.Hour
	lines := make(chan string, 4)
	s.chatListeners = append(s.chatListeners, lines)
	s.notifySender = func(string) error { return fmt.Errorf("grok acp: prompt already in flight") }
	_ = s.SendToOverseer(userTurnPrefix + "hello")
	select {
	case l := <-lines:
		t.Fatalf("busy defer announced: %s", l)
	default:
	}
}

// The owner echo carries the message id the undelivered/delivered frames key on.
func TestT806EchoCarriesMessageID(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(chatUserEchoID("hi", "om-1")), &m); err != nil {
		t.Fatal(err)
	}
	if m["msg_id"] != "om-1" || m["type"] != "user" {
		t.Fatalf("echo lacks msg_id: %v", m)
	}
	var plain map[string]any
	_ = json.Unmarshal([]byte(chatUserEcho("hi")), &plain)
	if _, ok := plain["msg_id"]; ok {
		t.Fatal("plain echo must not invent an id")
	}
}

// A send during an overseer relaunch gap waits a bounded moment for re-attach
// before it is nacked (the 04:20:09 specimen); zero wait nacks at once.
func TestT806AwaitOverseerProcessIsBounded(t *testing.T) {
	s := &Server{}
	start := time.Now()
	if s.awaitOverseerProcess() {
		t.Fatal("no process must not report running")
	}
	if time.Since(start) > 40*time.Millisecond {
		t.Fatal("zero wait blocked")
	}
	s.SetOverseerReattachWait(120 * time.Millisecond)
	start = time.Now()
	if s.awaitOverseerProcess() || time.Since(start) < 100*time.Millisecond {
		t.Fatal("did not wait out the bounded re-attach window")
	}
}
