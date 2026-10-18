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
)

// The agent list is the operator read. A blocked queue's depth, head age,
// and block reason have to be on that line, taken from the seat authority
// the queue itself publishes. Reading them off Backlog in the test would
// only prove the queue still knows.
func TestT7665AgentListShowsBlockedQueueStoppage(t *testing.T) {
	s, dir := t530Server(t)
	const name = "jv-stoppage"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-stop", Purpose: claudia.PurposeWork, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	enqueued := time.Now().Add(-2 * time.Hour).UTC()
	if _, _, err := s.sendQueue().Append(name, "owned attempt", enqueued); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append(name, "behind the attempt", enqueued.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(name)
	if err != nil || !ok || attempt.State != "attempting" {
		t.Fatalf("claim: %+v ok=%v err=%v", attempt, ok, err)
	}

	// No second queue read between the claim and this Get. The save that
	// recorded the attempt is what published the stoppage.
	st := s.seatState(name)
	if st.QueueDepth != 2 || st.QueueBlock == nil || *st.QueueBlock != "in flight" || !st.QueueOldest.Equal(enqueued) {
		block := "<nil>"
		if st.QueueBlock != nil {
			block = *st.QueueBlock
		}
		t.Fatalf("authority after claim: depth %d oldest %v block %q", st.QueueDepth, st.QueueOldest, block)
	}

	res, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := toolText(res)
	line := queueStoppageLine(body, name)
	if line == "" {
		t.Fatalf("agent list has no queue stoppage for %s:\n%s", name, body)
	}
	age := st.QueueOldestAge(time.Now()).Round(time.Second)
	for _, want := range []string{"2 queued", "oldest waiting " + age.String(), "block: in flight"} {
		if !strings.Contains(line, want) {
			t.Fatalf("stoppage line missing %q:\n%s", want, line)
		}
	}
}

// A claimable queue is not blocked. The authority still publishes the empty
// block claim, and the list shows depth and age without a block clause.
func TestT7665AgentListShowsClaimableQueueWithoutABlock(t *testing.T) {
	s, dir := t530Server(t)
	const name = "jv-claimable"
	if err := s.registry.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-claim", Purpose: claudia.PurposeWork, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	enqueued := time.Now().Add(-90 * time.Minute).UTC()
	if _, _, err := s.sendQueue().Append(name, "waiting", enqueued); err != nil {
		t.Fatal(err)
	}
	st := s.seatState(name)
	if st.QueueDepth != 1 || st.QueueBlock == nil || *st.QueueBlock != "" || !st.QueueOldest.Equal(enqueued) {
		block := "<nil>"
		if st.QueueBlock != nil {
			block = *st.QueueBlock
		}
		t.Fatalf("authority after admit: depth %d oldest %v block %q", st.QueueDepth, st.QueueOldest, block)
	}

	res, err := observedAgentList(s, context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	line := queueStoppageLine(toolText(res), name)
	if line == "" {
		t.Fatalf("agent list has no queue line:\n%s", toolText(res))
	}
	age := st.QueueOldestAge(time.Now()).Round(time.Second)
	if !strings.Contains(line, "1 queued") || !strings.Contains(line, "oldest waiting "+age.String()) {
		t.Fatalf("claimable line = %q", line)
	}
	if strings.Contains(line, "block:") {
		t.Fatalf("claimable queue invented a block: %s", line)
	}
}

func queueStoppageLine(body, name string) string {
	prefix := "QUEUE " + name + ":"
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, prefix) {
			return line
		}
	}
	return ""
}
