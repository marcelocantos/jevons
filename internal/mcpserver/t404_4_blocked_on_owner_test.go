// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T404.4 acceptance: jevons_agent_list surfaces a seat whose latest stored
// report is an owner-decision block as blocked_on_owner, naming the open
// question id and its silence_outcome — and the mark clears on the next
// list once the question is answered, with no restart.
func TestT4044AgentListSurfacesBlockedOnOwner(t *testing.T) {
	asker := &fakeSender{alive: false}
	s, _ := t404Server(t, map[string]*fakeSender{"jv-t404-auto": asker}, map[string]string{"jv-t404-auto": ""})
	repo := t.TempDir()
	s.storeAgentReport("jv-t404-auto", t404Report(repo, []string{"cockpit", "slack"}, "cockpit unless you say otherwise"))

	before, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	text := targetFileToolText(before)
	if !strings.Contains(text, "jv-t404-auto") || !strings.Contains(text, "blocked_on_owner") {
		t.Fatalf("list does not show blocked_on_owner status:\n%s", text)
	}
	if !strings.Contains(text, "surface@1") || !strings.Contains(text, "proceeds on default: cockpit unless you say otherwise") {
		t.Fatalf("list does not name the open question id and its silence_outcome:\n%s", text)
	}

	answered, err := s.handleOwnerQuestions(context.Background(), t404Req("answer", repo, map[string]any{"answer": "slack", "actor": "jevons"}))
	if err != nil || answered.IsError {
		t.Fatalf("answer: %+v %v", answered, err)
	}

	after, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("list after answer: %v", err)
	}
	afterText := targetFileToolText(after)
	if strings.Contains(afterText, "blocked_on_owner") {
		t.Fatalf("mark did not clear after the question was answered:\n%s", afterText)
	}
}
