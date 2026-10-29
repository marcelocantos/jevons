// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetpanel"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T1127: parked intent + live process surfaces as intent_fault on agent_list.
func TestT1127AgentListSurfacesParkedButRunningAsFault(t *testing.T) {
	s, dir := t401Server(t)
	const name = "jv-t1127-parked-live"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-" + name, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.intentStore().SetAgent(name, fleetintent.Parked, "jevons-po", "deliberate park", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Observed under the seat's own session: an observation with no session
	// is reset when the list path establishes the registry's session id, and
	// the seat then reads Unknown, which is not a live process.
	s.Seats().Observe(seatstate.Observation{
		Name: name, SessionID: "s-" + name, Alive: seatstate.Yes, LocalAlive: seatstate.Yes,
		QueueDepth: seatstate.QueueUnknown, Source: "test",
	})
	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := targetFileToolText(res)
	if !strings.Contains(body, fleetpanel.StatusRunningAgainstIntent) {
		t.Fatalf("agent_list must not show plain running; want %q in:\n%s", fleetpanel.StatusRunningAgainstIntent, body)
	}
	if !strings.Contains(body, "should not be running") {
		t.Fatalf("agent_list must name the intent fault:\n%s", body)
	}
	if strings.Contains(body, name+" running ") {
		t.Fatalf("agent_list still labels row as plain running:\n%s", body)
	}
}

// A parked seat nobody has observed (the minute after a restart, before the
// broker sweep) is not a live process, so it is not an intent fault.
func TestT1127UnobservedParkedSeatIsNotAFault(t *testing.T) {
	s, dir := t401Server(t)
	const name = "jv-t1127-parked-unobserved"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-" + name, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.intentStore().SetAgent(name, fleetintent.Parked, "jevons-po", "deliberate park", time.Now()); err != nil {
		t.Fatal(err)
	}
	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if body := targetFileToolText(res); strings.Contains(body, fleetpanel.StatusRunningAgainstIntent) {
		t.Fatalf("an unobserved parked seat must not read %q:\n%s", fleetpanel.StatusRunningAgainstIntent, body)
	}
}
