// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/rsi"
)

func dispositionTestServer(t *testing.T) (*Server, *rsi.Coach) {
	t.Helper()
	coach, err := rsi.NewCoach(rsi.CoachArgs{
		StateDir: t.TempDir(),
		Interval: -1,
		DryRun:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRSICoach(coach)
	return s, coach
}

func TestHandleRSIDispositionRecordAndMetrics(t *testing.T) {
	s, coach := dispositionTestServer(t)
	now := time.Now().UTC()
	if err := coach.Dispositions().RecordDelivered([]rsi.Judgment{
		{Fingerprint: "fp-1", Name: "gap one"},
		{Fingerprint: "fp-2", Name: "gap two"},
	}, now); err != nil {
		t.Fatal(err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"action":      "record",
		"fingerprint": "fp-1",
		"disposition": "ignore_with_reason",
		"reason":      "one-off venting",
	}
	res, err := s.handleRSIDisposition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if text := rsiToolText(res); !strings.Contains(text, "ignore_with_reason") {
		t.Fatalf("record response: %q", text)
	}

	req.Params.Arguments = map[string]any{"action": "metrics"}
	res, err = s.handleRSIDisposition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := rsiToolText(res)
	if !strings.Contains(text, "ignored=1") || !strings.Contains(text, "pending=1") {
		t.Fatalf("metrics text: %q", text)
	}

	req.Params.Arguments = map[string]any{"action": "list"}
	res, err = s.handleRSIDisposition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if text := rsiToolText(res); !strings.Contains(text, "fp-2") || !strings.Contains(text, "pending") {
		t.Fatalf("list text: %q", text)
	}
}

func TestHandleRSIDispositionValidation(t *testing.T) {
	s, _ := dispositionTestServer(t)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"action":      "record",
		"fingerprint": "fp-x",
		"disposition": "file", // no target_id
	}
	res, err := s.handleRSIDisposition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("want error for file without target_id, got %q", rsiToolText(res))
	}
}

// TestTargetFileQualityBar proves acceptance #3: coach-linked filings refuse
// bare phrase-friction leaves; concrete filings file and auto-record the
// disposition so the loop closes in one call.
func TestTargetFileQualityBar(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	runBullseye = func(args ...string) (string, error) {
		return "ok\nids: T421\n", nil
	}
	s, coach := dispositionTestServer(t)
	repo := t.TempDir()

	// Bare phrase-friction leaf with a fingerprint: refused.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd":         repo,
		"name":        "friction: timeout",
		"fingerprint": "fp-judge",
	}
	res, err := s.handleTargetFile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("bare friction filing must be refused, got %q", targetFileToolText(res))
	}
	if text := targetFileToolText(res); !strings.Contains(text, "T333") {
		t.Fatalf("refusal should cite the quality bar: %q", text)
	}

	// Concrete filing with evidence: accepted, disposition recorded.
	req.Params.Arguments = map[string]any{
		"cwd":         repo,
		"name":        "Owner-visible restart stalls are eliminated from the daily path",
		"acceptance":  "Hermetic restart fixture completes under 5s and live probe stays green",
		"fingerprint": "fp-judge",
		"evidence":    "session-abc corr=42",
	}
	res, err = s.handleTargetFile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := targetFileToolText(res)
	if !strings.Contains(text, "__TARGET_FILED__:T421") {
		t.Fatalf("want filed marker, got %q", text)
	}
	if !strings.Contains(text, "Disposition recorded") {
		t.Fatalf("want disposition confirmation, got %q", text)
	}
	entries, err := coach.Dispositions().Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Disposition != rsi.DispositionFile || entries[0].TargetID != "T421" {
		t.Fatalf("disposition entry wrong: %+v", entries)
	}

	// No fingerprint: light path unchanged, bare name only draws a downrank note.
	req.Params.Arguments = map[string]any{
		"cwd":  repo,
		"name": "friction: timeout",
	}
	res, err = s.handleTargetFile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text = targetFileToolText(res)
	if res.IsError {
		t.Fatalf("owner light path must not be blocked: %q", text)
	}
	if !strings.Contains(text, "quality note") {
		t.Fatalf("want downrank note on bare name, got %q", text)
	}
}
