// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/seatinert"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T754 — a seat that accepts sends and never advances is inert after the
// observation bound; a seat inside a long tool call is not.

type t754Clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *t754Clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *t754Clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestT754InertSeatAfterObservationBound(t *testing.T) {
	s, dir := t530Server(t)
	const name = "jevons-po"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-po",
		Purpose: claudia.PurposeWork, Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	s.SetSeatAliveFn(func(n string) bool { return n == name })
	clock := &t754Clock{t: time.Date(2026, 9, 21, 5, 35, 0, 0, time.UTC)}
	s.SetSweepClock(clock.now)
	lastMove := clock.now().Add(-5 * time.Minute)
	s.Seats().Observe(seatstate.Observation{
		Name: name, SessionID: "s-po", Alive: seatstate.Yes,
		LastActivity: lastMove, QueueDepth: seatstate.QueueUnknown, Source: "test",
	})
	enq := clock.now().Add(-3 * time.Minute)
	if _, _, err := s.sendQueue().Append(name, "refill order", enq); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append(name, "second refill", enq.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		clock.add(seatinert.MinObservationGap)
		s.observeInertSeats(clock.now())
	}
	line, ok := s.InertSeat(name)
	if !ok || !strings.HasPrefix(line, "INERT "+name) {
		t.Fatalf("InertSeat=%q ok=%v", line, ok)
	}
	res, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(res)
	if !strings.Contains(body, AgentStatusInert) || !strings.Contains(body, "INERT "+name) {
		t.Fatalf("agent_list missing inert classification:\n%s", body)
	}
}

func TestT754LongToolCallIsNotInert(t *testing.T) {
	s, dir := t530Server(t)
	const name = "jv-worker"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-w",
		Purpose: claudia.PurposeWork, Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	s.SetSeatAliveFn(func(n string) bool { return n == name })
	clock := &t754Clock{t: time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC)}
	s.SetSweepClock(clock.now)
	stale := clock.now().Add(-10 * time.Minute)
	s.Seats().Observe(seatstate.Observation{
		Name: name, SessionID: "s-w", Alive: seatstate.Yes,
		LastActivity: stale, QueueDepth: seatstate.QueueUnknown, Source: "test",
	})
	if _, _, err := s.sendQueue().Append(name, "behind long tool", clock.now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	s.wedges.moved(name, clock.now().Add(-30*time.Second))
	for i := 0; i < 4; i++ {
		clock.add(seatinert.MinObservationGap)
		s.observeInertSeats(clock.now())
		if _, ok := s.InertSeat(name); ok {
			t.Fatalf("long tool call sample %d classified inert", i)
		}
	}
}
