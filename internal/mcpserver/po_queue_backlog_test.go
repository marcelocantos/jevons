// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/roles"
	"github.com/marcelocantos/jevons/internal/staffops"
)

func TestT1182SentinelDetectsPOQueueBacklog(t *testing.T) {
	alive := map[string]bool{"jevons-po": true}
	s, dir := t1192Fixture(t)
	if err := s.recordAgentRole("jevons-po", roles.ProductOwner); err != nil {
		t.Fatal(err)
	}
	enqueued := time.Now().Add(-13 * time.Minute)
	for i := 0; i < 15; i++ {
		if _, _, err := s.sendQueue().Append("jevons-po", "direct "+string(rune('A'+i)), enqueued); err != nil {
			t.Fatal(err)
		}
	}
	rt := &sentinelRuntime{firstSeen: map[string]time.Time{}}
	obs := s.samplePOQueueBacklog(rt, "jevons", dir, time.Now(), time.Minute, func(name string) bool { return alive[name] })
	if len(obs) != 1 {
		t.Fatalf("obs=%v want one backlog", obs)
	}
	sigs := staffops.BuildSignals(staffops.ObserveInput{POQueueBacklog: obs})
	var found bool
	for _, sig := range sigs {
		if sig.Kind == "po_queue_backlog" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected po_queue_backlog signal")
	}
	for _, sig := range sigs {
		if sig.Kind != "po_queue_backlog" {
			continue
		}
		d := staffops.Classify(sig)
		if d.Action != staffops.ActionFilePO {
			t.Fatalf("want file+PO; decision=%+v", d)
		}
	}
}

func TestT1182AgentListSurfacesQueueBacklogFault(t *testing.T) {
	s, _ := t1192Fixture(t)
	s.ensurePOQueueBacklogFaultCache().set(poQueueBacklogFaultSnap{
		PO: "jevons-po", Depth: 15, OldestAge: 13 * time.Minute, At: time.Now(),
	})
	list, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(list)
	if !strings.Contains(body, AgentStatusPOQueueBacklog) {
		t.Fatalf("agent_list status missing typed fault:\n%s", body)
	}
}
