// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/sendq"
)

type requestIDSender struct {
	mu     sync.Mutex
	calls  []string
	legacy int
	busy   bool
}

func (f *requestIDSender) Send(string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.legacy++
	return nil
}
func (*requestIDSender) Interrupt() error { return nil }
func (*requestIDSender) Alive() bool      { return true }
func (f *requestIDSender) SendWithRequestID(text, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id+":"+text)
	return nil
}
func (f *requestIDSender) snapshot() ([]string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...), f.legacy
}

func TestT1054BusyQueueRestartDrainsSameRequestID(t *testing.T) {
	dir := t.TempDir()
	before, _, _ := t418Daemon(t, dir)
	busy := &requestIDSender{}
	before.noteTurnInFlight("a")
	observeSenderFixture(before, "a", busy)
	setObservedSenderResolver(before, func(string) (agentSender, bool, error) { return busy, false, nil })
	res, err := deliverToSenderModeWithRequestID(before, "a", "same text", "host-one", delivery.ModeSubmit, busy, false, confirmByCaller)
	if err != nil || res.Status != "queued" {
		t.Fatalf("busy delivery: %+v %v", res, err)
	}
	if _, legacy := busy.snapshot(); legacy != 0 {
		t.Fatal("legacy send called on busy delivery")
	}
	if _, _, err := before.sendQueue().AppendWithRequestID("a", "same text", "host-two", time.Now()); err != nil {
		t.Fatal(err)
	}
	after, _, _ := t418Daemon(t, dir)
	sender := &requestIDSender{}
	setObservedSenderResolver(after, func(string) (agentSender, bool, error) { return sender, false, nil })
	after.drainAgentSendQueue("a")
	calls, legacy := sender.snapshot()
	if len(calls) != 1 || calls[0] != "host-one:same text" || legacy != 0 {
		t.Fatalf("drain=%v legacy=%d", calls, legacy)
	}
	entries, err := after.sendQueue().Snapshot("a")
	if err != nil || len(entries) != 1 || entries[0].RequestID != "host-two" {
		t.Fatalf("next question lost: %+v %v", entries, err)
	}
}

func TestT1054PublicAdmissionResultPreservesHostIDWhenQueued(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	sender := &requestIDSender{}
	observeSenderFixture(s, "a", sender)
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return sender, false, nil })
	s.noteTurnInFlight("a")
	res, err := s.DeliverAgentMessageModeWithRequestID("a", "owner question", OriginOwner, delivery.ModeSubmit, "host-issued-question")
	if err != nil || res.Status != "queued" || res.RequestID != "host-issued-question" {
		t.Fatalf("admission: %+v %v", res, err)
	}
	entries, err := s.sendQueue().Snapshot("a")
	if err != nil || len(entries) != 1 || entries[0].RequestID != res.RequestID || entries[0].ID == res.RequestID {
		t.Fatalf("queued identity: %+v %v", entries, err)
	}
}

func TestT1054PinnedClaudiaNeverDropsIDIntoLegacySend(t *testing.T) {
	s, _, _ := t418Daemon(t, t.TempDir())
	legacy := &queueAttemptSender{send: func(string) error { t.Fatal("legacy Send called"); return nil }}
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return legacy, false, nil })
	observeSenderFixture(s, "a", legacy)
	_, err := deliverToSenderModeWithRequestID(s, "a", "question", "host-one", delivery.ModeSubmit, legacy, false, confirmByCaller)
	if !errors.Is(err, errRequestIDUnavailable) {
		t.Fatalf("no explicit T184 refusal: %v", err)
	}
	if entries, _ := s.sendQueue().Snapshot("a"); len(entries) != 0 {
		t.Fatalf("refused request was queued: %+v", entries)
	}
	// An old daemon may have persisted an ID before a pin rollback: a drain
	// must return it to Pending, never deliver untagged or mark it uncertain.
	if _, _, err := s.sendQueue().AppendWithRequestID("a", "question", "host-one", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.drainAgentSendQueue("a")
	entries, err := s.sendQueue().Snapshot("a")
	if err != nil || len(entries) != 1 || entries[0].State != sendq.Pending || entries[0].RequestID != "host-one" {
		t.Fatalf("rollback drain: %+v %v", entries, err)
	}
}

func TestT1054ModeSeamBindsIDPerInvocation(t *testing.T) {
	f := &requestModeFake{}
	a := sendModeRequestSeam(f, "host-A")
	b := sendModeRequestSeam(f, "host-B")
	if a == nil || b == nil {
		t.Fatal("typed mode seam unavailable")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a("one", delivery.ModeSteer) }()
	go func() { defer wg.Done(); b("two", delivery.ModeSteer) }()
	wg.Wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 2 || !(containsString(f.calls, "host-A:one") && containsString(f.calls, "host-B:two")) {
		t.Fatalf("cross-stamped calls: %v", f.calls)
	}
}

type requestModeFake struct{ requestIDSender }

func (f *requestModeFake) SendModeWithRequestIDString(text, mode, id string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id+":"+text)
	return "steer", "in_turn", nil
}
func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
