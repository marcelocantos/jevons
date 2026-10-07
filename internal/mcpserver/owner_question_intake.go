// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"encoding/json"
	"fmt"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
	"github.com/mark3labs/mcp-go/mcp"
)

// recordedGateQuestion returns the typed owner-question intake from a
// successful owner-gate record. Errors leave the existing gate response intact
// but surface the failure: consumers must not infer intake from ledger prose.
func recordedGateQuestion(cwd, target, question, by string, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	if result == nil || result.IsError {
		return result, nil
	}
	if strings.TrimSpace(by) == "" {
		by = "unknown"
	}
	q, err := ownerquestion.FromOwnerGate(cwd, target, question, by)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("owner gate recorded but question intake failed: %v", err)), nil
	}
	data, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	result.Content = append(result.Content, mcp.TextContent{Type: "text", Text: "owner-question intake: " + string(data)})
	return result, nil
}

// recordedGateQuestion notifies only after the ledger write succeeded. A
// transport failure is explicitly surfaced and retained as failed outbox state.
func (s *Server) recordedGateQuestion(cwd, target, question, by string, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	result, err := recordedGateQuestion(cwd, target, question, by, result)
	if err != nil || result == nil || result.IsError {
		return result, err
	}
	if s.stateDir == "" {
		return result, nil
	} // hermetic callers without daemon state
	q, err := ownerquestion.FromOwnerGate(cwd, target, question, by)
	if err != nil {
		return result, err
	}
	if s.ownerQuestionsDir != "" {
		if err := ownerquestionview.New(s.ownerQuestionsDir).Record(ownerquestionview.Question{
			Identity: ownerquestionview.Identity{Repo: q.Identity.Repo, Target: q.Identity.Target, ID: q.Identity.ID, Version: q.Identity.Version},
			Text:     q.Text, Asker: q.Asker, AnswerRoute: q.AnswerRoute,
		}); err != nil {
			result.Content = append(result.Content, mcp.TextContent{Type: "text", Text: "question view intake FAILED: " + err.Error()})
		}
	}
	entry, err := ownerquestions.New(s.stateDir).Observe(ownerquestions.Question{Key: q.Identity.Repo + "#" + q.Identity.Target + "#" + q.Identity.ID, Text: q.Text}, time.Now())
	if err != nil {
		result.Content = append(result.Content, mcp.TextContent{Type: "text", Text: "notification FAILED (retry retained): " + err.Error()})
		return result, nil
	}
	result.Content = append(result.Content, mcp.TextContent{Type: "text", Text: fmt.Sprintf("notification %s (spooled, NOT owner-delivered): %s", entry.Status, entry.SpoolPath)})
	return result, nil
}
func (s *Server) resolveGateNotification(cwd, target string) {
	if s.stateDir == "" {
		return
	}
	repo, err := ownerquestion.CanonicalRepo(cwd)
	if err != nil {
		return
	}
	if s.ownerQuestionsDir != "" {
		if err := s.resolveGateQuestion(repo, target, "owner gate answered"); err != nil {
			fmt.Printf("owner gate question view close failed: %v\n", err)
		}
	}
	if err := ownerquestions.New(s.stateDir).Resolve(repo + "#" + target + "#owner-gate"); err != nil {
		fmt.Printf("owner gate notification close failed: %v\n", err)
	}
}
