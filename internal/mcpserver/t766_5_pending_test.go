// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// pendingAgentSends counts a claimable tail and skips an in-flight or
// uncertain attempt. handleAgentKill still refuses a non-overseer kill
// from the full depth. Wiring the kill callback back to pendingAgentSends
// drops the seat while the attempt is held.
func TestT7665PendingOmitsHeldAndKillStillRefuses(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)
	const child = "jv-t530-drain"
	if _, err := s.enqueueAgentSend(child, "in flight copy that kill must still see"); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(child)
	if err != nil || !ok {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	if _, err := s.enqueueAgentSend(child, "behind the attempt"); err != nil {
		t.Fatal(err)
	}
	if got, held := s.pendingAgentSends(child), s.heldAgentSends(child); got != 1 || held != 2 {
		t.Fatalf("in flight pending=%d held=%d; want the claimable tail only", got, held)
	}
	if s.pendingAgentSends("never-observed") != seatstate.QueueUnknown {
		t.Fatal("an unobserved seat became a pending count")
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": child, "actor": "jevons-po"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(res), "T530") {
		t.Fatalf("in-flight kill = %s", toolText(res))
	}
	if s.registry.Def(child) == nil {
		t.Fatal("in-flight kill removed the seat")
	}

	if err := s.sendQueue().Resolve(child, attempt, sendq.Unverified, "no receipt"); err != nil {
		t.Fatal(err)
	}
	if got, held := s.pendingAgentSends(child), s.heldAgentSends(child); got != 1 || held != 2 {
		t.Fatalf("uncertain pending=%d held=%d; the tail stays pending and the attempt stays held", got, held)
	}
	res, err = s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(toolText(res), "T530") {
		t.Fatalf("uncertain kill = %s", toolText(res))
	}
	if s.registry.Def(child) == nil {
		t.Fatal("uncertain kill removed the seat")
	}
	rows, err := s.sendQueue().Snapshot(child)
	if err != nil || len(rows) != 2 || rows[0].Text != "in flight copy that kill must still see" {
		t.Fatalf("queue after refused kills = %+v %v", rows, err)
	}

	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if _, err := s.sendQueue().Reconcile(child, rows[0].ID, rows[0].AttemptID, sendq.ReconcileRequeue, "owner", "transcript has no copy", now); err != nil {
		t.Fatal(err)
	}
	if got, held := s.pendingAgentSends(child), s.heldAgentSends(child); got != 2 || held != 2 {
		t.Fatalf("requeue pending=%d held=%d; a requeued copy is pending", got, held)
	}
}
