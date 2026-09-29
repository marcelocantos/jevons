// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/notice"
)

// 🎯T254.4: a worker's stored terminal report produces a durable structured
// notice visible to the parent, queryable independently of the raw
// transcript. This pins the real seam — Server.storeAgentReport, the same
// call site notify uses to persist the report before delivery — not just the
// pure internal/notice extraction logic.

func finishReportFixture(target, sha, gate string) string {
	return "```jevons\n" +
		"jevons: kind finish-report\n" +
		"jevons: target " + target + "\n" +
		"jevons: sha " + sha + "\n" +
		"jevons: gate id=" + gate + "\n" +
		"jevons: verdict GREEN\n" +
		"jevons: silent-ledger none\n" +
		"```\n\nLanded it. GATE make-test-go exit=0 GREEN id=" + gate + ".\n"
}

func TestStoreAgentReportRecordsStructuredNoticeForParent(t *testing.T) {
	s, _, dir := reportServer(t)
	s.registry = newLineageRegistry(t, map[string]string{
		"jv-worker-a": "jevons-po",
	})

	s.storeAgentReport("jv-worker-a", finishReportFixture("T254.4", "abc123", "deadbeef"))

	notices, err := notice.List(dir, "jevons-po")
	if err != nil {
		t.Fatalf("notice.List: %v", err)
	}
	if len(notices) != 1 {
		t.Fatalf("expected 1 structured notice for jevons-po, got %d: %+v", len(notices), notices)
	}
	n := notices[0]
	if n.Agent != "jv-worker-a" {
		t.Fatalf("agent = %q", n.Agent)
	}
	if n.Outcome != notice.OutcomeDone {
		t.Fatalf("outcome = %q, want done", n.Outcome)
	}
	if n.SHA != "abc123" || n.GateID != "id=deadbeef" {
		t.Fatalf("evidence not carried through: %+v", n)
	}
}

func TestStoreAgentReportNonTerminalDoesNotAddNotice(t *testing.T) {
	s, _, dir := reportServer(t)
	s.registry = newLineageRegistry(t, map[string]string{
		"jv-worker-b": "jevons-po",
	})

	s.storeAgentReport("jv-worker-b", "still working on it, no envelope here")

	notices, err := notice.List(dir, "jevons-po")
	if err != nil {
		t.Fatalf("notice.List: %v", err)
	}
	if len(notices) != 0 {
		t.Fatalf("plain-prose report must not mint a structured notice, got %+v", notices)
	}
}

func TestInboxListToolReturnsStructuredSummary(t *testing.T) {
	s, _, _ := reportServer(t)
	s.registry = newLineageRegistry(t, map[string]string{
		"jv-worker-c": "jevons-po",
	})
	s.storeAgentReport("jv-worker-c", finishReportFixture("T999", "feed", "f00d"))

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"parent": "jevons-po"}
	res, err := s.handleInboxList(context.Background(), req)
	if err != nil {
		t.Fatalf("handleInboxList: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleInboxList returned an error result: %+v", res)
	}
	text := toolResultText(res)
	if !strings.Contains(text, "jv-worker-c") || !strings.Contains(text, "outcome=done") {
		t.Fatalf("unexpected inbox listing: %s", text)
	}
}

// The overseer's view: omitting parent lists every parent's notices,
// including one filed as unowned because its reporter had no known parent.
func TestInboxListToolFleetWideForOverseer(t *testing.T) {
	s, _, _ := reportServer(t)
	s.registry = newLineageRegistry(t, map[string]string{
		"jv-worker-d": "jevons-po",
		"jv-worker-e": "other-po",
	})
	s.storeAgentReport("jv-worker-d", finishReportFixture("T1", "aaa", "a1"))
	s.storeAgentReport("jv-worker-e", finishReportFixture("T2", "bbb", "b2"))
	s.storeAgentReport("jv-reaped-f", finishReportFixture("T3", "ccc", "c3"))

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleInboxList(context.Background(), req)
	if err != nil || res.IsError {
		t.Fatalf("handleInboxList: %v %+v", err, res)
	}
	text := inboxText(res)
	for _, want := range []string{"jv-worker-d", "jv-worker-e", "jv-reaped-f", "overseer view"} {
		if !strings.Contains(text, want) {
			t.Fatalf("fleet-wide listing missing %q:\n%s", want, text)
		}
	}

	req.Params.Arguments = map[string]any{"parent": "jevons-po", "outcome": "done"}
	res, _ = s.handleInboxList(context.Background(), req)
	text = inboxText(res)
	if !strings.Contains(text, "jv-worker-d") || strings.Contains(text, "jv-worker-e") {
		t.Fatalf("parent-scoped listing wrong:\n%s", text)
	}

	req.Params.Arguments = map[string]any{"outcome": "finished"}
	res, _ = s.handleInboxList(context.Background(), req)
	if !res.IsError {
		t.Fatalf("unknown outcome must be refused, got %s", inboxText(res))
	}
}

// inboxText is the whole tool text; toolResultText caps at 500 bytes, which
// cuts a multi-notice listing short.
func inboxText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
