// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// Agent list and the fleet-health sweep share the pin line. An attempt this
// daemon owns blocks the seat, so that line must not say later messages deliver.
func TestT7665OwnedAttemptBlocksThePinLine(t *testing.T) {
	s, dir := t530Server(t)
	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	const name = "jv-owned-pin"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-pin", Purpose: claudia.PurposeWork, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Hour).UTC()
	if _, _, err := s.sendQueue().Append(name, "uncertain sibling", now); err != nil {
		t.Fatal(err)
	}
	held, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok {
		t.Fatalf("claim held: %+v %v %v", held, ok, err)
	}
	if err := s.sendQueue().Resolve(name, held, sendq.Unverified, "fate unknown"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append(name, "owned attempt", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	owned, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok || owned.Text != "owned attempt" {
		t.Fatalf("claim owned: %+v %v %v", owned, ok, err)
	}
	if _, _, err := s.sendQueue().Append(name, "behind the attempt", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	res, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(res)
	for _, want := range []string{held.AttemptID, "blocks the seat"} {
		if !strings.Contains(body, want) {
			t.Fatalf("agent list omits %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "keep delivering") {
		t.Fatalf("agent list promises flow-past while an attempt is owned:\n%s", body)
	}

	s.SweepSendBacklogs()
	notes := strings.Join(up.all(), "\n")
	for _, want := range []string{held.AttemptID, "blocks the seat", "[Fleet health]"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("fleet health omits %q:\n%s", want, notes)
		}
	}
	if strings.Contains(notes, "keep delivering") {
		t.Fatalf("fleet health promises flow-past while an attempt is owned:\n%s", notes)
	}
	rows, err := s.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 3 || rows[0].State != sendq.Uncertain || rows[0].Text != "uncertain sibling" || rows[1].ID != owned.ID || rows[1].State != sendq.Attempting || rows[2].State != sendq.Pending || rows[2].Text != "behind the attempt" {
		t.Fatalf("sweep moved the queue: %+v %v", rows, err)
	}
}
