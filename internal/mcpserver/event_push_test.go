// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"github.com/marcelocantos/jevons/internal/sendq"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if n := observedPendingSends(s, "jevons-po"); n != 1 {
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
	if n := observedPendingSends(s, "jevons-po"); n != 1 {
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
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) {
		return fs, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	s.drainAgentSendQueue("jevons-po")
	want := butler.FormatEventPush("worker-finished", "slice A landed")
	if len(fs.sent) != 1 || fs.sent[0] != want {
		t.Fatalf("drained sent=%v want [%q]", fs.sent, want)
	}
	if n := observedPendingSends(s, "jevons-po"); n != 0 {
		t.Fatalf("pending after drain=%d", n)
	}
}

// T1050: an explicitly scoped permission held during a turn is never offered
// after a newer direct hold, even if the hold used a different send path.
func TestT1050QueuedEventAuthorizationDirectHold(t *testing.T) {
	p := &pushFakeParticipants{names: map[string]bool{"worker": true}}
	s := &Server{butler: newPushButler(t, t.TempDir(), &pushFakeFleet{}, p)}
	s.noteTurnInFlight("worker")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"target": "worker", "event": "owner-authorization", "text": "permission to report", "directive_family": "T1050", "directive_kind": "authorization"}
	res, err := s.handleEventPush(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("auth: %v %s", err, toolText(res))
	}
	entries, _ := s.sendQueue().Snapshot("worker")
	if len(entries) != 1 || entries[0].Directive == nil {
		t.Fatalf("typed auth not queued: %+v", entries)
	}
	// The direct sender is a different channel; the queue contract is shared.
	_, depth, removed, err := s.sendQueue().ApplyDirective("worker", "owner hold", sendq.Directive{Family: "T1050", Kind: "hold"}, false, time.Now())
	if err != nil || depth != 0 || len(removed) != 1 {
		t.Fatalf("direct hold: depth=%d removed=%+v err=%v", depth, removed, err)
	}
	if _, ok, err := s.sendQueue().ClaimFront("worker"); ok || err != nil {
		t.Fatalf("stale auth claimable: %v %v", ok, err)
	}
}

func TestT1050DirectEventHoldCancelsQueuedPermission(t *testing.T) {
	p := &pushFakeParticipants{names: map[string]bool{"worker": true}}
	s := &Server{butler: newPushButler(t, t.TempDir(), &pushFakeFleet{}, p)}
	auth := sendq.Directive{Family: "report/T1050", Kind: "authorization"}
	_, _, _, err := s.sendQueue().ApplyDirective("worker", "[event: owner] permission", auth, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"target": "worker", "event": "owner-hold", "text": "do not report", "directive_family": "report/T1050", "directive_kind": "hold"}
	res, err := s.handleEventPush(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("hold: %v %s", err, toolText(res))
	}
	if !strings.Contains(p.last, "do not report") {
		t.Fatalf("hold not delivered: %q", p.last)
	}
	entries, _ := s.sendQueue().Snapshot("worker")
	if len(entries) != 0 {
		t.Fatalf("stale auth still queued: %+v", entries)
	}
}

// Exact incident shape: an untyped busy owner authorization, followed by an
// untyped direct owner hold, remains unsafe. This is a policy blocker, not a
// passing supersession oracle. No text classifier is allowed to turn these
// human messages into typed operations without a migrated producer contract.
func TestT1050UntypedIncidentStillUnsafe(t *testing.T) {
	worker := &fakeSender{alive: true}
	s, _ := chainServer(t, map[string]*fakeSender{"worker": worker})
	s.registry = newLineageRegistry(t, map[string]string{"jevons-po": "jevons", "worker": "jevons-po"})
	p := &pushFakeParticipants{names: map[string]bool{"worker": true}}
	s.butler = newPushButler(t, t.TempDir(), &pushFakeFleet{}, p)
	s.noteTurnInFlight("worker")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"target": "worker", "event": "owner-authorization", "text": "You may send your report now"}
	result, err := s.handleEventPush(context.Background(), req)
	if err != nil || result.IsError {
		t.Fatalf("busy permission: %v %s", err, toolText(result))
	}
	// The turn ends, then a plain direct jevons_agent_send hold arrives before
	// the queue's next drain. Both API entry points are exercised, not modeled
	// by manually editing the queue or passing text through a fake classifier.
	s.noteTurnEnded("worker")
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "jevons-po", "text": "Hold: do not send that report"}
	hold, err := s.handleAgentSend(context.Background(), req)
	if err != nil || hold.IsError {
		t.Fatalf("direct hold: %v %s", err, toolText(hold))
	}
	if len(worker.sent) != 1 || !strings.Contains(worker.sent[0], "Hold: do not send that report") {
		t.Fatalf("direct hold not delivered: %+v", worker.sent)
	}
	entry, ok, err := s.sendQueue().ClaimFront("worker")
	if err != nil || !ok || !strings.Contains(entry.Text, "You may send your report now") {
		t.Fatalf("untyped old permission should still be claimable: %+v %v %v", entry, ok, err)
	}
}

func TestT1050RepeatedTypedHoldCancelsNewPermission(t *testing.T) {
	p := &pushFakeParticipants{names: map[string]bool{"jevons": true}}
	s := &Server{butler: newPushButler(t, t.TempDir(), &pushFakeFleet{}, p)}
	auth := sendq.Directive{Family: "review", Kind: "authorization"}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"target": "jevons", "event": "owner-hold", "text": "hold review", "directive_family": "review", "directive_kind": "hold"}
	for i := 0; i < 2; i++ {
		_, _, _, err := s.sendQueue().ApplyDirective("jevons", "permission", auth, true, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.handleEventPush(context.Background(), req)
		if err != nil || res.IsError {
			t.Fatalf("hold %d: %v %s", i, err, toolText(res))
		}
		entries, _ := s.sendQueue().Snapshot("jevons")
		if len(entries) != 0 {
			t.Fatalf("hold %d left stale permission: %+v", i, entries)
		}
	}
}

func TestT1050TypedBusyEventAndDirectAgentHold(t *testing.T) {
	worker := &fakeSender{alive: true}
	s, _ := chainServer(t, map[string]*fakeSender{"worker": worker})
	s.registry = newLineageRegistry(t, map[string]string{"jevons-po": "jevons", "worker": "jevons-po"})
	s.butler = newPushButler(t, t.TempDir(), &pushFakeFleet{}, &pushFakeParticipants{names: map[string]bool{"worker": true}})
	s.noteTurnInFlight("worker")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"target": "worker", "event": "owner-authorization", "text": "permission", "directive_family": "report/T1050", "directive_kind": "authorization"}
	result, err := s.handleEventPush(context.Background(), req)
	if err != nil || result.IsError {
		t.Fatalf("auth: %v %s", err, toolText(result))
	}
	s.noteTurnEnded("worker")
	req.Params.Arguments = map[string]any{"name": "worker", "actor": "jevons-po", "text": "hold", "directive_family": "report/T1050", "directive_kind": "hold"}
	hold, err := s.handleAgentSend(context.Background(), req)
	if err != nil || hold.IsError {
		t.Fatalf("hold: %v %s", err, toolText(hold))
	}
	if len(worker.sent) != 1 || !strings.Contains(worker.sent[0], "hold") {
		t.Fatalf("hold not delivered: %+v", worker.sent)
	}
	if _, ok, err := s.sendQueue().ClaimFront("worker"); ok || err != nil {
		t.Fatalf("old auth offered after hold: %v %v", ok, err)
	}
}
