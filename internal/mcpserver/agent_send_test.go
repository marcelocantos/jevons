// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"
)

// fakeSender implements agentSender for 🎯T111.1 hermetic busy/queue tests.
type fakeSender struct {
	alive      bool
	inFlight   bool
	sent       []string
	interrupts int
	// afterInterruptClears makes the next Send succeed after Interrupt.
	afterInterruptClears bool
}

func (f *fakeSender) Alive() bool { return f.alive }

func (f *fakeSender) Send(text string) error {
	if !f.alive {
		return fmt.Errorf("not running")
	}
	if f.inFlight {
		return fmt.Errorf("grok acp: prompt already in flight")
	}
	f.sent = append(f.sent, text)
	f.inFlight = true
	return nil
}

func (f *fakeSender) Interrupt() error {
	f.interrupts++
	if f.afterInterruptClears {
		f.inFlight = false
	}
	return nil
}

func TestIsPromptInFlight(t *testing.T) {
	if !isPromptInFlight(fmt.Errorf("send to x: grok acp: prompt already in flight")) {
		t.Fatal("expected match")
	}
	if isPromptInFlight(fmt.Errorf("other")) {
		t.Fatal("unexpected match")
	}
	if isPromptInFlight(nil) {
		t.Fatal("nil")
	}
}

func TestDeliverToSenderQueuesWhenBusy(t *testing.T) {
	s := &Server{}
	fs := &fakeSender{alive: true, inFlight: true}
	res, err := deliverToSender(s, "po", "nudge fan-out", false, fs, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued", res.Status)
	}
	if res.Queued != 1 {
		t.Fatalf("queued=%d", res.Queued)
	}
	if !strings.Contains(res.Message, "queued") || !strings.Contains(res.Message, "interrupt=true") {
		t.Fatalf("message should describe recovery, got %q", res.Message)
	}
	if len(fs.sent) != 0 {
		t.Fatal("should not have sent while busy without interrupt")
	}
	// Second nudge stacks.
	res2, err := deliverToSender(s, "po", "second", false, fs, false)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Queued != 2 {
		t.Fatalf("queued=%d want 2", res2.Queued)
	}
}

func TestDeliverToSenderInterruptThenSend(t *testing.T) {
	s := &Server{}
	fs := &fakeSender{alive: true, inFlight: true, afterInterruptClears: true}
	res, err := deliverToSender(s, "po", "force nudge", true, fs, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Status != "interrupted_sent" {
		t.Fatalf("status=%q want interrupted_sent", res.Status)
	}
	if fs.interrupts != 1 {
		t.Fatalf("interrupts=%d", fs.interrupts)
	}
	if len(fs.sent) != 1 || fs.sent[0] != "force nudge" {
		t.Fatalf("sent=%v", fs.sent)
	}
}

func TestDeliverToSenderInterruptStillBusyQueues(t *testing.T) {
	s := &Server{}
	// Interrupt does not clear inFlight — stuck ACP flag; must still queue.
	fs := &fakeSender{alive: true, inFlight: true, afterInterruptClears: false}
	res, err := deliverToSender(s, "po", "nudge", true, fs, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "interrupted_queued" {
		t.Fatalf("status=%q", res.Status)
	}
	if res.Queued != 1 {
		t.Fatalf("queued=%d", res.Queued)
	}
}

func TestDeliverToSenderHappyPath(t *testing.T) {
	s := &Server{}
	fs := &fakeSender{alive: true}
	res, err := deliverToSender(s, "w", "hello", false, fs, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "sent" {
		t.Fatalf("status=%q", res.Status)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("sent=%v", fs.sent)
	}
}

func TestDrainAgentSendQueue(t *testing.T) {
	s := &Server{}
	s.enqueueAgentSend("po", "queued-msg")
	// Without registry, drain is a no-op for process but we still need Get.
	// Manual drain via dequeue only.
	got := s.dequeueAgentSend("po")
	if got != "queued-msg" {
		t.Fatalf("got %q", got)
	}
	if s.dequeueAgentSend("po") != "" {
		t.Fatal("expected empty")
	}
}
