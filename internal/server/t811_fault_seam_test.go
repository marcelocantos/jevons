// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func TestT811FaultSeamConsumesOneRefusalPerOwnerAttempt(t *testing.T) {
	dir := t.TempDir()
	s := &Server{}
	owner := userTurnPrefix + "hi"
	if s.injectedBrokerFault(owner) != nil {
		t.Fatal("unarmed seam injected a fault")
	}
	s.EnableBrokerFaultSeam(dir)
	if s.injectedBrokerFault(owner) != nil {
		t.Fatal("armed seam without a fault file injected a fault")
	}
	if err := os.WriteFile(filepath.Join(dir, brokerFaultFile), []byte("2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.injectedBrokerFault("[event: worker-idle]") != nil {
		t.Fatal("fleet note consumed the owner fault")
	}
	for i := 0; i < 2; i++ {
		err := s.injectedBrokerFault(owner)
		if err == nil || !strings.Contains(err.Error(), "not_owner") || notifyErrClass(err) != "not_owner" {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if s.injectedBrokerFault(owner) != nil {
		t.Fatal("fault outlived its count")
	}
}

// 🎯T920: J33 on a Claude isolate. The overseer cannot steer, so an owner
// message waits behind the running turn, and an earlier journey's owner
// message was still queued ahead of J33's. The journey arms the seam with its
// own token: the leftover must deliver untouched and the refusal — and the
// undelivered frame the cockpit paints — must land on J33's message, whose
// Resend then delivers it once.
func TestT920TokenFaultRefusesTheJourneysMessageNotTheQueueHead(t *testing.T) {
	dir := t.TempDir()
	s := &Server{}
	s.notifyRetryDelay = time.Hour // Resend drives the retry here
	s.EnableBrokerFaultSeam(dir)
	lines := make(chan string, 32)
	s.chatListeners = append(s.chatListeners, lines)
	var delivered []string
	s.notifySender = func(text string) error {
		delivered = append(delivered, text)
		return nil
	}
	frames := func() string {
		var got []string
		for len(lines) > 0 {
			var m map[string]any
			_ = json.Unmarshal([]byte(<-lines), &m)
			if m["type"] == "send_error" {
				got = append(got, fmt.Sprint(m["state"], ":", m["msg_id"]))
			}
		}
		return strings.Join(got, ",")
	}
	endTurn := func() { s.HandleAgentEvent(claudia.Event{Type: "assistant", StopReason: "end_turn"}) }

	// The overseer is mid-turn; an earlier journey's owner message waits.
	s.mu.Lock()
	s.waiting = true
	s.mu.Unlock()
	_ = s.SendToOverseerAs(userTurnPrefix+"Reply with exactly: j32-leftover", "om-left")

	// J33 arms the seam with its token, then sends.
	const token = "t811-1790675703576"
	if err := os.WriteFile(filepath.Join(dir, brokerFaultFile), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = s.SendToOverseerAs(userTurnPrefix+"Reply with exactly: "+token, "om-j33")

	endTurn() // leftover drains
	if len(delivered) != 1 || !strings.Contains(delivered[0], "j32-leftover") {
		t.Fatalf("the leftover must deliver untouched, delivered %q", delivered)
	}
	endTurn() // J33's message is offered and refused
	if got := frames(); got != "undelivered:om-j33" {
		t.Fatalf("the refusal must land on the journey's message: frames %q", got)
	}
	if len(delivered) != 1 {
		t.Fatalf("J33's message delivered through the fault: %q", delivered)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, brokerFaultFile)); strings.TrimSpace(string(b)) != "0" {
		t.Fatalf("token fault not consumed: %q", b)
	}
	if err := s.ResendOwnerMessage("om-j33"); err != nil {
		t.Fatal(err)
	}
	if got := frames(); got != "delivered:om-j33" {
		t.Fatalf("resend frames %q", got)
	}
	if len(delivered) != 2 || !strings.Contains(delivered[1], token) {
		t.Fatalf("resend must deliver J33's message once, delivered %q", delivered)
	}
}

// A token fault ignores owner text without the token and fleet notes, and
// fires once.
func TestT920TokenFaultFiresOnceOnMatchingOwnerText(t *testing.T) {
	dir := t.TempDir()
	s := &Server{}
	s.EnableBrokerFaultSeam(dir)
	if err := os.WriteFile(filepath.Join(dir, brokerFaultFile), []byte("t811-42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.injectedBrokerFault(userTurnPrefix+"something else") != nil {
		t.Fatal("owner text without the token was refused")
	}
	if s.injectedBrokerFault("[event: worker-idle] t811-42") != nil {
		t.Fatal("fleet note consumed the owner fault")
	}
	if err := s.injectedBrokerFault(userTurnPrefix + "Reply with exactly: t811-42"); err == nil || notifyErrClass(err) != "not_owner" {
		t.Fatalf("matching owner text: %v", err)
	}
	if s.injectedBrokerFault(userTurnPrefix+"Reply with exactly: t811-42") != nil {
		t.Fatal("token fault fired twice")
	}
}
