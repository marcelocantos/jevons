// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

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
