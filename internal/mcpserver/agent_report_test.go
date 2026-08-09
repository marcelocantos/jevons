// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T388 product-path oracle. The pure elision/store logic is pinned in
// internal/agentreport; these tests pin the wiring at the seam where the
// content was actually being lost — Server.notify, which cut every report at
// text[:1997]+"..." with no marker and no way to get the rest.
//
// The fixture is the shape of the three reports cut on 2026-08-09: a long
// body of working notes with the correction and the asks at the END.

func overBoundReport() string {
	var b strings.Builder
	b.WriteString("# jv-t372-auto — EC-6 harness inventory\n\n## 1. What I did\n\n")
	for b.Len() < 8*1024 {
		b.WriteString("Checked each inventory claim against source, not the summary table.\n")
	}
	b.WriteString("\n## 2. Correction — changes the EC-6 ruling\n\n")
	b.WriteString("The inventory doc (§2.3 F-HARNESS-1) tells the opposite story.\n")
	b.WriteString("\n## 3. Asks\n\n- MatchTurn needs an owner decision before I go further.\n")
	return b.String()
}

// reportServer builds a Server with the overseer delivery seam captured and
// the durable store rooted in a temp dir.
func reportServer(t *testing.T) (*Server, *string, string) {
	t.Helper()
	s := &Server{}
	dir := t.TempDir()
	s.SetAgentReportDir(dir)
	var delivered string
	s.SetNotify(func(text string) { delivered = text })
	return s, &delivered, dir
}

// Acceptance 1 + 4: an over-bound report delivered through the real notify
// path is never silently cut. RED against the pre-fix tree, where the
// delivered text ended in a bare "..." and the store did not exist.
func TestNotifyOverBoundReportIsMarkedAndRetrievable(t *testing.T) {
	s, delivered, dir := reportServer(t)
	report := overBoundReport()

	s.notify("jv-t372-auto", report)

	if *delivered == "" {
		t.Fatalf("nothing delivered to the overseer")
	}
	if !agentreport.IsTruncatedDelivery(*delivered) {
		t.Fatalf("over-bound report delivered without a truncation marker — the silent cut:\n%s", *delivered)
	}
	// The tail is what the overseer needed and what the old head-only cut ate.
	for _, want := range []string{"## 3. Asks", "MatchTurn needs an owner decision"} {
		if !strings.Contains(*delivered, want) {
			t.Errorf("delivery lost the tail (%q missing)", want)
		}
	}

	// The handle in the marker must actually resolve. A marker without a
	// working retrieval is visibility without recoverability.
	recs, err := agentreport.List(dir, "jv-t372-auto")
	if err != nil || len(recs) != 1 {
		t.Fatalf("store has %d records (err %v), want 1", len(recs), err)
	}
	if !strings.Contains(*delivered, recs[0].ID) {
		t.Errorf("marker does not name the stored report id %s:\n%s", recs[0].ID, *delivered)
	}
	full, err := agentreport.Latest(dir, "jv-t372-auto")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if full.Text != report {
		t.Errorf("stored report is not the whole report (%d vs %d bytes)", len(full.Text), len(report))
	}
}

// Over-broadness mutation guard: a short report must be delivered exactly as
// before — no marker, no retrieval handle, no added chrome. A fix that marks
// every delivery as truncated, or that appends a handle unconditionally,
// fails here.
func TestNotifyShortReportIsUnchanged(t *testing.T) {
	s, delivered, _ := reportServer(t)
	report := "Landed 🎯T999 at abc1234. `go test ./internal/foo/` ok. No asks."

	s.notify("jv-t999-x", report)

	want := "[Agent jv-t999-x responded]\n" + report
	if *delivered != want {
		t.Errorf("short report delivery changed:\n got: %q\nwant: %q", *delivered, want)
	}
	if agentreport.IsTruncatedDelivery(*delivered) {
		t.Errorf("short report acquired a truncation marker it does not need")
	}
	if strings.Contains(*delivered, "jevons_agent_report_read") {
		t.Errorf("short report acquired a retrieval handle it does not need")
	}
}

// Acceptance 2: the report is stored BEFORE delivery, so a 🎯T165/T195 reap
// firing on the same terminal event cannot take undelivered content with it.
// The read path consults no registry — this Server has none — which is
// precisely why it answers after the agent is gone, where jevons_agent_send
// answers "agent is not running".
func TestReportReadableWithNoRegistryAfterDeregistration(t *testing.T) {
	s, _, _ := reportServer(t)
	report := overBoundReport()

	// Drive the real event sink, the path that both notifies and reaps.
	sink := s.agentEventSink("jv-t372-auto")
	sink(claudia.Event{Type: "assistant", Text: report, StopReason: "end_turn"})

	if s.registry != nil {
		t.Fatalf("fixture should have no registry")
	}
	res, err := s.handleAgentReportRead(context.Background(), reportReadRequest(map[string]any{
		"name": "jv-t372-auto",
	}))
	if err != nil {
		t.Fatalf("handleAgentReportRead: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "MatchTurn needs an owner decision") {
		t.Errorf("full read did not return the tail of the report:\n%s", tailOf(text))
	}
	if !strings.Contains(text, "## 1. What I did") {
		t.Errorf("full read did not return the head of the report")
	}
}

// Acceptance 3: a named section comes back verbatim from the store, so the
// overseer re-requesting one part never makes the agent re-derive (and so
// possibly rewrite) what it already said.
func TestReportSectionReadReturnsStoredBytes(t *testing.T) {
	s, _, _ := reportServer(t)
	report := overBoundReport()
	s.notify("jv-t372-auto", report)

	res, err := s.handleAgentReportRead(context.Background(), reportReadRequest(map[string]any{
		"name":    "jv-t372-auto",
		"section": "asks",
	}))
	if err != nil {
		t.Fatalf("handleAgentReportRead: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "MatchTurn needs an owner decision") {
		t.Errorf("section read lost its content:\n%s", text)
	}
	if strings.Contains(text, "## 1. What I did") {
		t.Errorf("section read returned the whole report instead of the asked-for section")
	}

	// A missing section names what is available rather than returning nothing.
	res, err = s.handleAgentReportRead(context.Background(), reportReadRequest(map[string]any{
		"name":    "jv-t372-auto",
		"section": "no such heading",
	}))
	if err != nil {
		t.Fatalf("handleAgentReportRead: %v", err)
	}
	if !res.IsError {
		t.Errorf("a missing section must be an error, not silent empty text")
	}
}

// With no store configured the delivery must still be marked as cut. A
// misconfigured state dir may cost the handle; it must never restore the
// silent truncation.
func TestUnstoredOverBoundReportIsStillMarked(t *testing.T) {
	s := &Server{}
	var delivered string
	s.SetNotify(func(text string) { delivered = text })

	s.notify("jv-x", overBoundReport())

	if !agentreport.IsTruncatedDelivery(delivered) {
		t.Errorf("unstored over-bound report was cut without a marker")
	}
	if strings.Contains(delivered, "jevons_agent_report_read") {
		t.Errorf("marker offers a retrieval call that cannot work without a store")
	}
}

func reportReadRequest(args map[string]any) mcp.CallToolRequest {
	var req mcp.CallToolRequest
	req.Params.Name = "jevons_agent_report_read"
	req.Params.Arguments = args
	return req
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatalf("nil tool result")
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func tailOf(s string) string {
	if len(s) > 400 {
		return "…" + s[len(s)-400:]
	}
	return s
}
