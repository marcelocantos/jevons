// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/mcpserver"
)

// TestT1074BlockedOwnerHoldsOverseerNotifyDrain drives the production chain:
// DeliverAgentMessageMode → deliverToOverseer → DeliverToOverseerAs →
// drainOverseerNotes, with the drain gate taken from AllowFleetControl.
// A blocked_owner fleet must not call the sender, must keep the text, and
// must not report delivered_unconfirmed. Lifting intent drains once.
func TestT1074BlockedOwnerHoldsOverseerNotifyDrain(t *testing.T) {
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetFleet(fleetintent.BlockedOwner, "owner", "pause", time.Now()); err != nil {
		t.Fatal(err)
	}
	mcp := &mcpserver.Server{}
	mcp.SetFleetIntentStore(store)

	var sent []string
	httpSrv := &Server{}
	httpSrv.notifySender = func(text string) error {
		sent = append(sent, text)
		return nil
	}
	httpSrv.SetAllowNotifyDrain(func() bool {
		return mcp.AllowFleetControl("jevons", fleetintent.ControlDeliverSubmit).Allow
	})
	mcp.SetOverseerDeliver(func(text string, origin mcpserver.SendOrigin) error {
		return httpSrv.DeliverToOverseerAs(text, string(origin))
	})
	mcp.SetOverseerNotifyDrain(httpSrv.DrainOverseerNotes)

	const token = "pause-hold-token"
	res, err := mcp.DeliverAgentMessageMode("jevons", token, mcpserver.OriginAgent, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("sender called while blocked_owner: %v", sent)
	}
	queued := notifyQueueSnapshot(httpSrv)
	if !strings.Contains(strings.Join(queued, "\n"), token) {
		t.Fatalf("queue dropped the text: %q", queued)
	}
	if res.Status == "delivered_unconfirmed" {
		t.Fatalf("status=%q message=%s", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "NOT delivered") || !strings.Contains(res.Message, "fleet intent") {
		t.Fatalf("hold message = %q", res.Message)
	}

	if err := mcp.SetFleetIntent(fleetintent.Working, "owner", "lift"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], token) {
		t.Fatalf("lift drain sent = %q", sent)
	}
}

// TestT1074WorkingFleetOverseerSendStillUnconfirmed keeps the busy-queue
// contract: a working fleet's overseer send still reaches the sender and
// reports delivered_unconfirmed when no receipt settles.
func TestT1074WorkingFleetOverseerSendStillUnconfirmed(t *testing.T) {
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mcp := &mcpserver.Server{}
	mcp.SetFleetIntentStore(store)

	var sent []string
	httpSrv := &Server{}
	httpSrv.notifySender = func(text string) error {
		sent = append(sent, text)
		return nil
	}
	httpSrv.SetAllowNotifyDrain(func() bool {
		return mcp.AllowFleetControl("jevons", fleetintent.ControlDeliverSubmit).Allow
	})
	mcp.SetOverseerDeliver(func(text string, origin mcpserver.SendOrigin) error {
		return httpSrv.DeliverToOverseerAs(text, string(origin))
	})

	res, err := mcp.DeliverAgentMessageMode("jevons", "working-token", mcpserver.OriginAgent, delivery.ModeSubmit)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("sender calls = %d", len(sent))
	}
	if res.Status != "delivered_unconfirmed" {
		t.Fatalf("status=%q message=%s", res.Status, res.Message)
	}
	if strings.Contains(res.Message, "NOT delivered") {
		t.Fatalf("working fleet reported a hold: %s", res.Message)
	}
}

func notifyQueueSnapshot(s *Server) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.notifyQueue))
	copy(out, s.notifyQueue)
	return out
}
