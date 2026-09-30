// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestParsePlanSteps(t *testing.T) {
	steps, err := parsePlanSteps("design schema|schema exists; hermetic test|test green")
	if err != nil {
		t.Fatalf("parsePlanSteps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Name != "design schema" || steps[0].Acceptance != "schema exists" {
		t.Fatalf("unexpected step 0: %+v", steps[0])
	}
	if steps[1].Name != "hermetic test" || steps[1].Acceptance != "test green" {
		t.Fatalf("unexpected step 1: %+v", steps[1])
	}
}

func TestParsePlanStepsNewlineSeparated(t *testing.T) {
	steps, err := parsePlanSteps("one|a\ntwo|b\n")
	if err != nil {
		t.Fatalf("parsePlanSteps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
}

func TestParsePlanStepsEmpty(t *testing.T) {
	if _, err := parsePlanSteps("   ;  ; "); err == nil {
		t.Fatalf("expected error for empty steps")
	}
}

func newPlanStepTestServer(t *testing.T) *Server {
	t.Helper()
	planStepStoreMu.Lock()
	planStepStore = nil
	planStepStoreMu.Unlock()
	t.Cleanup(func() {
		planStepStoreMu.Lock()
		planStepStore = nil
		planStepStoreMu.Unlock()
	})
	return &Server{stateDir: t.TempDir()}
}

func callPlanStep(t *testing.T, s *Server, args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := s.handlePlanStep(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePlanStep: %v", err)
	}
	if res == nil {
		t.Fatalf("handlePlanStep: nil result")
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if res.IsError {
		t.Fatalf("handlePlanStep returned error: %s", b.String())
	}
	return b.String()
}

// TestPlanStepToolClaimResumeAcrossRestart is the MCP-level acceptance
// oracle for 🎯T254.3's "implementers claim ready steps only; mid-target
// restart resumes the in-progress step without owner re-brief" line: it
// drives the tool exactly as an agent would, then simulates a daemon
// restart by dropping the process-wide store cache and re-claiming.
func TestPlanStepToolClaimResumeAcrossRestart(t *testing.T) {
	s := newPlanStepTestServer(t)

	setOut := callPlanStep(t, s, map[string]any{
		"op":     "set",
		"target": "T254.3",
		"steps":  "design step graph|schema exists; claim/resume|hermetic passes",
	})
	if !strings.Contains(setOut, "progress=0/2") {
		t.Fatalf("unexpected set output: %s", setOut)
	}

	claimOut := callPlanStep(t, s, map[string]any{
		"op":       "claim",
		"target":   "T254.3",
		"claimant": "jv-worker-1",
	})
	if !strings.Contains(claimOut, "id=T254.3.step1") || !strings.Contains(claimOut, "status=in_progress") {
		t.Fatalf("unexpected claim output: %s", claimOut)
	}

	// Simulate a daemon restart: drop the cached in-process store so the
	// next call constructs a fresh one from the same on-disk state dir.
	planStepStoreMu.Lock()
	planStepStore = nil
	planStepStoreMu.Unlock()

	resumeOut := callPlanStep(t, s, map[string]any{
		"op":       "claim",
		"target":   "T254.3",
		"claimant": "jv-worker-1-resumed",
	})
	if !strings.Contains(resumeOut, "id=T254.3.step1") {
		t.Fatalf("expected resume onto step1 after restart, got: %s", resumeOut)
	}
	if !strings.Contains(resumeOut, `claimed_by="jv-worker-1"`) {
		t.Fatalf("expected original claimant preserved across restart, got: %s", resumeOut)
	}

	completeOut := callPlanStep(t, s, map[string]any{
		"op":      "complete",
		"target":  "T254.3",
		"step_id": "T254.3.step1",
	})
	if !strings.Contains(completeOut, "progress=1/2") {
		t.Fatalf("unexpected complete output: %s", completeOut)
	}

	nextOut := callPlanStep(t, s, map[string]any{
		"op":       "claim",
		"target":   "T254.3",
		"claimant": "jv-worker-1-resumed",
	})
	if !strings.Contains(nextOut, "id=T254.3.step2") {
		t.Fatalf("expected step2 to unblock, got: %s", nextOut)
	}
}
