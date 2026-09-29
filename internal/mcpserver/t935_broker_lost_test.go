// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T935: a seat the broker took down that came back on a fresh session, or
// did not come back within the bound, is reported to its parent and to the
// overseer — the notice names the seat, what happened, and what to do.
func TestT935BrokerLostOutcomeReachesParentAndOverseer(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	now := time.Date(2026, 9, 30, 0, 15, 0, 0, time.UTC)

	s.noteBrokerLostOutcome(fleet.ReattachResult{
		Back: []string{"jv-t928-mcp-attach"},
		Fresh: []fleet.LostSession{{
			Name: "jv-t928-mcp-attach", Parent: "jevons-po", Provider: "anthropic", TargetID: "T928",
			OldSession: "fe8f1413-8636-466c-be11-d98e57124921", NewSession: "15768aca-61c3-43f4-ab2b-976c3774e7cc",
		}},
	}, now)
	if len(po.sent) != 1 || len(inbox.texts) != 1 {
		t.Fatalf("fresh notice: parent got %d, overseer got %d; want one each", len(po.sent), len(inbox.texts))
	}
	for _, want := range []string{"jv-t928-mcp-attach", "FRESH session", "re-send its brief", "force_rebrief=true", "fe8f1413"} {
		if !strings.Contains(po.sent[0], want) {
			t.Fatalf("parent fresh notice missing %q: %s", want, po.sent[0])
		}
	}

	po.inFlight = false
	s.noteTurnEnded("jevons-po")
	s.noteBrokerLostOutcome(fleet.ReattachResult{Stuck: []fleet.BrokerLostStuck{{
		Name: "jv-t928-mcp-attach", Parent: "jevons-po", Since: now.Add(-5 * time.Minute), Attempts: 16,
		Err: "broker protocol: agent_failed: context deadline exceeded",
	}}}, now)
	if len(po.sent) != 2 || len(inbox.texts) != 2 {
		t.Fatalf("stuck notice: parent got %d, overseer got %d; want two each", len(po.sent), len(inbox.texts))
	}
	for _, want := range []string{"has not come back", "16 relaunch attempts over 5m0s", "context deadline exceeded", "jevons_agent_kill name=jv-t928-mcp-attach force=true"} {
		if !strings.Contains(po.sent[1], want) {
			t.Fatalf("parent stuck notice missing %q: %s", want, po.sent[1])
		}
	}
}

// A PO whose parent is the overseer is told once, on the fleet-health channel.
func TestT935OverseerChildIsToldOnce(t *testing.T) {
	s, inbox := chainServer(t, map[string]*fakeSender{})
	s.noteBrokerLostOutcome(fleet.ReattachResult{Stuck: []fleet.BrokerLostStuck{{
		Name: "jevons-po", Parent: "jevons", Since: time.Now().Add(-6 * time.Minute), Attempts: 18, Err: "down",
	}}}, time.Now())
	if len(inbox.texts) != 1 || !strings.Contains(inbox.texts[0], "jevons-po") {
		t.Fatalf("overseer inbox = %v, want one notice naming the seat", inbox.texts)
	}
}
