// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestParseBullseyeTrackID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ok\nids: T110\nchanged: true", "T110"},
		{"Filed 🎯T12.3 — name", "T12.3"},
		{"noise T99 more", "T99"},
		{"no id here", ""},
	}
	for _, tc := range cases {
		if got := parseBullseyeTrackID(tc.in); got != tc.want {
			t.Errorf("parseBullseyeTrackID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHandleTargetFileUsesBullseye(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	var saw []string
	runBullseye = func(args ...string) (string, error) {
		saw = append([]string{}, args...)
		return "ok\nids: T111\n", nil
	}
	s := New(t.TempDir(), nil, nil)
	// Register tool via SetButler-less path: call handle directly.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd":        t.TempDir(),
		"name":       "Images paste works",
		"acceptance": "Paste shows preview",
		"context":    "from target: aside",
	}
	res, err := s.handleTargetFile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := targetFileToolText(res)
	if !strings.Contains(text, "__TARGET_FILED__:T111") {
		t.Fatalf("want filed marker, got %q", text)
	}
	if !strings.Contains(text, "🎯T111") {
		t.Fatalf("want 🎯 id in confirmation, got %q", text)
	}
	joined := strings.Join(saw, " ")
	if !strings.Contains(joined, "commit") || !strings.Contains(joined, "Images paste works") {
		t.Fatalf("bullseye args unexpected: %v", saw)
	}
}

// 🎯T226: file twice with same mission always allocates a new id (no attach).
func TestHandleTargetFileAlwaysAllocatesNewID(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })

	trackCalls := 0
	runBullseye = func(args ...string) (string, error) {
		trackCalls++
		return "ok\nids: T221\n", nil
	}

	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd":        t.TempDir(),
		"name":       "RHS inspect user-MD for fleet injects",
		"acceptance": "user injects and MD-shaped user turns render",
	}
	res, err := s.handleTargetFile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := targetFileToolText(res)
	if trackCalls != 1 {
		t.Fatalf("must always track (trackCalls=%d)", trackCalls)
	}
	if !strings.Contains(text, "__TARGET_FILED__:T221") {
		t.Fatalf("want new id T221, got %q", text)
	}
	if strings.Contains(text, "no new id allocated") || strings.Contains(text, "Attached to existing") {
		t.Fatalf("must not attach: %q", text)
	}
}

func targetFileToolText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
