// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/delivery"
)

type acceptingSender struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (f *acceptingSender) Alive() bool      { return true }
func (f *acceptingSender) Interrupt() error { return nil }
func (f *acceptingSender) Send(string) error {
	if f.calls.Add(1) == 1 {
		close(f.entered)
		<-f.release
	}
	return nil
}

// A second caller must not submit while the first socket write is still
// pending. OMP reports a prompt collision asynchronously, after Send returns.
func TestAgentSendSerializesConcurrentSubmissions(t *testing.T) {
	s := &Server{}
	f := &acceptingSender{entered: make(chan struct{}), release: make(chan struct{})}
	type result struct {
		status string
		err    error
	}
	first := make(chan result, 1)
	second := make(chan result, 1)
	go func() {
		res, err := deliverToSenderMode(s, "po", "first", delivery.ModeSubmit, f, false, confirmByCaller)
		first <- result{res.Status, err}
	}()
	<-f.entered
	go func() {
		res, err := deliverToSenderMode(s, "po", "second", delivery.ModeSubmit, f, false, confirmByCaller)
		second <- result{res.Status, err}
	}()
	const collisionWindow = 100 * time.Millisecond
	select {
	case got := <-second:
		close(f.release)
		<-first
		t.Fatalf("second submission raced the first: %+v", got)
	case <-time.After(collisionWindow):
	}
	close(f.release)
	if got := <-first; got.err != nil || got.status != "sent" {
		t.Fatalf("first=%+v", got)
	}
	if got := <-second; got.err != nil || got.status != "queued" {
		t.Fatalf("second=%+v", got)
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("provider submissions=%d, want one", got)
	}
}

type phaseOnlySender struct{ calls int }

func (f *phaseOnlySender) Alive() bool                  { return true }
func (f *phaseOnlySender) Interrupt() error             { return nil }
func (f *phaseOnlySender) Send(string) error            { f.calls++; return nil }
func (f *phaseOnlySender) TurnPhase() claudia.TurnPhase { return claudia.TurnInTurn }

func TestAgentSendQueuesWhenProviderKnowsTurnButDaemonDoesNot(t *testing.T) {
	s := &Server{}
	f := &phaseOnlySender{}
	res, err := deliverToSenderMode(s, "po", "close-out", delivery.ModeSubmit, f, false, confirmByCaller)
	if err != nil || res.Status != "queued" || res.Queued != 1 || f.calls != 0 {
		t.Fatalf("send=%+v err=%v provider calls=%d", res, err, f.calls)
	}
}

// slogCapture records Info-level records for 🎯T120.2 field assertions.
// Handle can run on a daemon goroutine (the send-queue drain) while the test
// reads, so a test that logs asynchronously reads through snapshot (🎯T910).
type slogCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *slogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *slogCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *slogCapture) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]slog.Record(nil), h.records...)
}

// reset discards what has been captured so far.
func (h *slogCapture) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
}

func (h *slogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *slogCapture) WithGroup(string) slog.Handler      { return h }

func attrsMap(r slog.Record) map[string]any {
	m := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})
	return m
}

// fakeSender implements agentSender for 🎯T111.1 hermetic busy/queue tests.
type fakeSender struct {
	alive      bool
	inFlight   bool
	sent       []string
	interrupts int
	// afterInterruptClears makes the next Send succeed after Interrupt.
	afterInterruptClears bool
	// sendErr forces Send to fail (🎯T305 delivery failure hermetic).
	sendErr error
	// sendHook runs after the send is accepted, while the caller still
	// holds product locks (🎯T627.4 admission observation).
	sendHook func()
}

func (f *fakeSender) Alive() bool { return f.alive }

func (f *fakeSender) Send(text string) error {
	if !f.alive {
		return fmt.Errorf("not running")
	}
	if f.sendErr != nil {
		return f.sendErr
	}
	if f.inFlight {
		return fmt.Errorf("grok acp: prompt already in flight")
	}
	f.sent = append(f.sent, text)
	f.inFlight = true
	if f.sendHook != nil {
		f.sendHook()
	}
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
	// Grok ACP (historical).
	if !isPromptInFlight(fmt.Errorf("send to x: grok acp: prompt already in flight")) {
		t.Fatal("expected Grok ACP match")
	}
	// 🎯T214 J6: non-Grok busy shapes must queue, not hard-fail.
	if !isPromptInFlight(fmt.Errorf("task worker-1 is busy")) {
		t.Fatal("expected Task busy match")
	}
	// 🎯T766.4: this used an invented "prompt in progress" phrasing no
	// backend produces; codex's own busy refusal is the real non-ACP shape.
	if !isPromptInFlight(fmt.Errorf("codex app-server: turn already in flight")) {
		t.Fatal("expected codex busy match")
	}
	if isPromptInFlight(fmt.Errorf("other")) {
		t.Fatal("unexpected match")
	}
	if isPromptInFlight(nil) {
		t.Fatal("nil")
	}
}

// 🎯T214 J6: deliverToSender queues on non-Grok busy strings (not only ACP).
func TestDeliverToSenderQueuesOnTaskBusy(t *testing.T) {
	s := &Server{}
	fs := &busyStringSender{alive: true, busyErr: "task abc is busy"}
	res, err := deliverToSender(s, "po", "nudge", false, fs, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued", res.Status)
	}
	if res.Queued != 1 {
		t.Fatalf("queued=%d", res.Queued)
	}
}

// busyStringSender returns a configurable busy error on Send.
type busyStringSender struct {
	alive   bool
	busyErr string
	sent    []string
}

func (f *busyStringSender) Alive() bool { return f.alive }

func (f *busyStringSender) Send(text string) error {
	if f.busyErr != "" {
		return fmt.Errorf("%s", f.busyErr)
	}
	f.sent = append(f.sent, text)
	return nil
}

func (f *busyStringSender) Interrupt() error { return nil }

func TestDeliverToSenderQueuesWhenBusy(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

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
	if !strings.Contains(res.Message, "queued") || !strings.Contains(res.Message, "mode=interrupt") {
		t.Fatalf("message should describe recovery, got %q", res.Message)
	}
	if len(fs.sent) != 0 {
		t.Fatal("should not have sent while busy without interrupt")
	}
	// 🎯T120.2: structured slog on success path (not MCP text alone).
	if len(cap.snapshot()) < 1 {
		t.Fatal("expected agent_send slog record")
	}
	got := attrsMap(cap.snapshot()[0])
	if got["component"] != "agent_send" || got["name"] != "po" || got["status"] != "queued" {
		t.Fatalf("slog attrs=%v", got)
	}
	if got["queued"] != int64(1) && got["queued"] != 1 {
		// slog may store int as int64 depending on Value.Any()
		if q, ok := got["queued"].(int); !ok || q != 1 {
			if q64, ok := got["queued"].(int64); !ok || q64 != 1 {
				t.Fatalf("queued attr=%v (%T)", got["queued"], got["queued"])
			}
		}
	}
	if got["rehydrated"] != false {
		t.Fatalf("rehydrated=%v", got["rehydrated"])
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
	// 🎯T416: an interrupt clears the turn, so the send that follows is judged
	// strictly. Witness the payload landing — the point here is the interrupt
	// mechanics, not the confirmation.
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
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

// TestDeliverToSenderInterruptStillBusyQueues used to pin T111.1's
// "OR RE-QUEUES IF STILL BUSY" fallback — the hole 🎯T424 closes.
// Revised, not deleted: interrupt=true must ERROR and must not enqueue.
func TestDeliverToSenderInterruptStillBusyQueues(t *testing.T) {
	s := &Server{}
	// Interrupt does not clear inFlight — stuck ACP flag.
	fs := &fakeSender{alive: true, inFlight: true, afterInterruptClears: false}
	res, err := deliverToSender(s, "po", "nudge", true, fs, false)
	if err == nil {
		t.Fatalf("interrupt still queued: status=%q queued=%d — 🎯T424 forbids this", res.Status, res.Queued)
	}
	if !strings.Contains(err.Error(), "not queued") {
		t.Fatalf("error %q does not say the message was not queued", err)
	}
	if strings.Contains(err.Error(), "interrupt=true") {
		t.Fatalf("interrupt path advised interrupt=true: %q", err)
	}
	if res.Queued != 0 {
		t.Fatalf("queued=%d want 0", res.Queued)
	}
}

func TestT424InterruptOnStoppedQueueDoesNotEnqueue(t *testing.T) {
	s := &Server{}
	for i := 0; i < 6; i++ {
		if _, err := s.enqueueAgentSend("po", fmt.Sprintf("m%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	fs := &fakeSender{alive: true, inFlight: true, afterInterruptClears: false}
	_, err := deliverToSender(s, "po", "seventh", true, fs, false)
	if err == nil {
		t.Fatal("interrupt added to a stuck queue")
	}
	if n := s.pendingAgentSends("po"); n != 6 {
		t.Fatalf("queue grew to %d, want 6", n)
	}
}

func TestT424InterruptIgnoresStaleInFlightAndDelivers(t *testing.T) {
	s := &Server{}
	s.noteTurnInFlight("po")
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	// Process is actually idle — the flag is the 2026-08-10 stale reading.
	fs := &fakeSender{alive: true, inFlight: false}
	res, err := deliverToSender(s, "po", "unstick", true, fs, false)
	if err != nil {
		t.Fatalf("stale in-flight + interrupt should deliver: %v", err)
	}
	if len(fs.sent) != 1 || fs.sent[0] != "unstick" {
		t.Fatalf("sent=%v", fs.sent)
	}
	if res.Status == "queued" || res.Queued > 0 {
		t.Fatalf("interrupt queued behind a stale reading: %+v", res)
	}
}

func TestDeliverToSenderHappyPath(t *testing.T) {
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := &Server{}
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	fs := &fakeSender{alive: true}
	res, err := deliverToSender(s, "w", "hello", false, fs, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "rehydrated_sent" {
		t.Fatalf("status=%q", res.Status)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("sent=%v", fs.sent)
	}
	got := attrsMap(cap.snapshot()[0])
	if got["status"] != "rehydrated_sent" || got["rehydrated"] != true || got["component"] != "agent_send" {
		t.Fatalf("slog attrs=%v", got)
	}
}

func TestDrainAgentSendQueue(t *testing.T) {
	s := &Server{}
	if _, err := s.enqueueAgentSend("po", "queued-msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Without registry, drain is a no-op for process but we still need Get.
	// Manual drain via dequeue only.
	got := s.dequeueAgentSend("po")
	if got.Text != "queued-msg" {
		t.Fatalf("got %q", got.Text)
	}
	if s.dequeueAgentSend("po").Text != "" {
		t.Fatal("expected empty")
	}
}
