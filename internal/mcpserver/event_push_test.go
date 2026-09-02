// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// pushFakeFleet for event-push thread path.
type pushFakeFleet struct {
	alive    map[string]bool
	launches int
	mintID   string
}

func (f *pushFakeFleet) Launch(t *thread.Thread) error {
	f.launches++
	if t.SessionID == "" {
		t.SessionID = f.mintID
	}
	if f.alive == nil {
		f.alive = map[string]bool{}
	}
	f.alive[t.ID] = true
	return nil
}
func (f *pushFakeFleet) Send(id, text string) (string, error) {
	return "reply:" + text, nil
}
func (f *pushFakeFleet) Alive(id string) bool { return f.alive[id] }
func (f *pushFakeFleet) Stop(id string)       { f.alive[id] = false }
func (f *pushFakeFleet) Remove(id string)     { delete(f.alive, id) }

type pushFakeParticipants struct {
	names map[string]bool
	last  string
}

func (p *pushFakeParticipants) Exists(id string) bool { return p.names[id] }
func (p *pushFakeParticipants) Deliver(id, text string) (string, error) {
	p.last = text
	return "agent-ack:" + text, nil
}

func newPushButler(t *testing.T, dir string, f butler.Fleet, p butler.Participants) *butler.Butler {
	t.Helper()
	store, err := thread.NewStore(filepath.Join(dir, "threads.json"))
	if err != nil {
		t.Fatal(err)
	}
	return butler.New(butler.Config{
		Store:        store,
		Scanner:      discovery.NewScanner(filepath.Join(dir, "projects")),
		Reader:       transcript.NewReader(filepath.Join(dir, "projects")),
		Fleet:        f,
		Participants: p,
	})
}

// 🎯T111.2: thread target via PushEvent (MCP butler path).
func TestEventPushThreadTarget(t *testing.T) {
	ff := &pushFakeFleet{mintID: "sess-thread"}
	dir := t.TempDir()
	b := newPushButler(t, dir, ff, nil)
	if _, err := b.Spawn(butler.SpawnArgs{ID: "po-thread", WorkDir: dir, Parent: "jevons"}); err != nil {
		t.Fatal(err)
	}
	reply, err := b.PushEvent("po-thread", "worker-finished", "slice A landed")
	if err != nil {
		t.Fatalf("PushEvent: %v", err)
	}
	if !strings.Contains(reply, "[event: worker-finished]") {
		t.Fatalf("reply=%q", reply)
	}
}

// 🎯T111.2: agent-only name succeeds; never "no thread".
func TestEventPushAgentOnlyTarget(t *testing.T) {
	dir := t.TempDir()
	p := &pushFakeParticipants{names: map[string]bool{"jevons-po": true}}
	b := newPushButler(t, dir, &pushFakeFleet{}, p)
	reply, err := b.PushEvent("jevons-po", "timer", "tick")
	if err != nil {
		t.Fatalf("PushEvent: %v", err)
	}
	if strings.Contains(errString(err), "no thread") {
		t.Fatal("must not say no thread for valid agent")
	}
	if !strings.Contains(p.last, "[event: timer]") || !strings.Contains(p.last, "tick") {
		t.Fatalf("payload=%q", p.last)
	}
	if !strings.Contains(reply, "agent-ack:") {
		t.Fatalf("reply=%q", reply)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// 🎯T111.2: unknown target is not a silent success.
func TestEventPushMissingBoth(t *testing.T) {
	dir := t.TempDir()
	b := newPushButler(t, dir, &pushFakeFleet{}, &pushFakeParticipants{names: map[string]bool{}})
	_, err := b.PushEvent("ghost", "ci", "green")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "no participant") && !strings.Contains(err.Error(), "no thread") {
		t.Fatalf("got %v", err)
	}
}

// busyPushParticipants returns a provider-busy error on Deliver (🎯T620).
type busyPushParticipants struct {
	names    map[string]bool
	err      error
	delivers int
	last     string
}

func (p *busyPushParticipants) Exists(id string) bool { return p.names[id] }

func (p *busyPushParticipants) Deliver(id, text string) (string, error) {
	p.delivers++
	p.last = text
	if p.err != nil {
		return "", p.err
	}
	return "agent-ack:" + text, nil
}

func pushCall(t *testing.T, s *Server, target, event, text string) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"target": target, "event": event, "text": text}
	res, err := s.handleEventPush(context.Background(), req)
	if err != nil {
		t.Fatalf("handleEventPush: %v", err)
	}
	return res
}

func assertQueuedPush(t *testing.T, res *mcp.CallToolResult, target string) {
	t.Helper()
	if res.IsError {
		t.Fatalf("busy push was a delivery error: %q", toolText(res))
	}
	got := toolText(res)
	if !strings.Contains(got, "queued") {
		t.Fatalf("want queued, got %q", got)
	}
	if strings.Contains(got, "grok acp: prompt already in flight") {
		t.Fatalf("queued result leaked the ACP busy string: %q", got)
	}
	if !strings.Contains(got, target) {
		t.Fatalf("queued result should name %q: %q", target, got)
	}
}

// 🎯T620: in-flight send is queued, never toolFailure with grok ACP busy text.
func TestT620EventPushQueuesWhenPromptInFlight(t *testing.T) {
	p := &busyPushParticipants{
		names: map[string]bool{"jevons-po": true},
		err:   fmt.Errorf("grok acp: prompt already in flight"),
	}
	s := &Server{butler: newPushButler(t, t.TempDir(), &pushFakeFleet{}, p)}
	res := pushCall(t, s, "jevons-po", "worker-finished", "slice A landed")
	assertQueuedPush(t, res, "jevons-po")
	if p.delivers != 1 {
		t.Fatalf("delivers=%d want 1 (flight unknown, so the provider is asked)", p.delivers)
	}
	if n := s.pendingAgentSends("jevons-po"); n != 1 {
		t.Fatalf("pending=%d want 1", n)
	}
	want := butler.FormatEventPush("worker-finished", "slice A landed")
	entries, err := s.sendQueue().Snapshot("jevons-po")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Text != want {
		t.Fatalf("queued wire=%q want %q", entries[0].Text, want)
	}
}

// 🎯T620: a known in-flight turn is not offered to the provider at all.
func TestT620EventPushQueuesWhenFlightInFlightWithoutCallingDeliver(t *testing.T) {
	p := &busyPushParticipants{
		names: map[string]bool{"jevons-po": true},
		err:   fmt.Errorf("grok acp: prompt already in flight"),
	}
	s := &Server{butler: newPushButler(t, t.TempDir(), &pushFakeFleet{}, p)}
	s.noteTurnInFlight("jevons-po")
	res := pushCall(t, s, "jevons-po", "overseer-direct", "continue T620")
	assertQueuedPush(t, res, "jevons-po")
	if p.delivers != 0 {
		t.Fatalf("Deliver called %d times; FlightInFlight must not hit the provider", p.delivers)
	}
	if n := s.pendingAgentSends("jevons-po"); n != 1 {
		t.Fatalf("pending=%d want 1", n)
	}
}

// 🎯T620: a queued push drains on turn-complete as the FormatEventPush wire.
func TestT620EventPushQueuedDrainsOnTurnComplete(t *testing.T) {
	p := &busyPushParticipants{
		names: map[string]bool{"jevons-po": true},
		err:   fmt.Errorf("grok acp: prompt already in flight"),
	}
	s := &Server{butler: newPushButler(t, t.TempDir(), &pushFakeFleet{}, p)}
	res := pushCall(t, s, "jevons-po", "worker-finished", "slice A landed")
	assertQueuedPush(t, res, "jevons-po")

	fs := &fakeSender{alive: true}
	s.SetSenderResolver(func(string) (agentSender, bool, error) {
		return fs, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	s.drainAgentSendQueue("jevons-po")
	want := butler.FormatEventPush("worker-finished", "slice A landed")
	if len(fs.sent) != 1 || fs.sent[0] != want {
		t.Fatalf("drained sent=%v want [%q]", fs.sent, want)
	}
	if n := s.pendingAgentSends("jevons-po"); n != 0 {
		t.Fatalf("pending after drain=%d", n)
	}
}
