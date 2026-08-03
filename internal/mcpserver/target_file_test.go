// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/targetfile"
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
	prevLoad := loadOpenLeavesForFile
	t.Cleanup(func() { loadOpenLeavesForFile = prevLoad })
	// Empty open set so unique names track normally.
	loadOpenLeavesForFile = func(cwd string) ([]targetfile.OpenLeaf, error) {
		return nil, nil
	}
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

// 🎯T222: file twice with same mission fixture → one id (no second track).
func TestHandleTargetFileDedupesNearDuplicate(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	prevLoad := loadOpenLeavesForFile
	t.Cleanup(func() { loadOpenLeavesForFile = prevLoad })

	loadOpenLeavesForFile = func(cwd string) ([]targetfile.OpenLeaf, error) {
		return []targetfile.OpenLeaf{{
			ID:         "T220",
			Name:       "RHS inspect renders fleet user injects as markdown",
			Acceptance: []string{"user injects and MD-shaped user turns render"},
			Status:     "identified",
		}}, nil
	}
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
	if trackCalls != 0 {
		t.Fatalf("must not allocate second id (trackCalls=%d)", trackCalls)
	}
	if !strings.Contains(text, "__TARGET_FILED__:T220") {
		t.Fatalf("want attach to T220, got %q", text)
	}
	if !strings.Contains(text, "🎯T220") || !strings.Contains(text, "no new id allocated") {
		t.Fatalf("attach message: %q", text)
	}
}

// 🎯T222 residual: force=true allocates despite near-dup.
func TestHandleTargetFileForceSplit(t *testing.T) {
	prev := runBullseye
	t.Cleanup(func() { runBullseye = prev })
	prevLoad := loadOpenLeavesForFile
	t.Cleanup(func() { loadOpenLeavesForFile = prevLoad })

	loadOpenLeavesForFile = func(cwd string) ([]targetfile.OpenLeaf, error) {
		return []targetfile.OpenLeaf{{
			ID: "T220", Name: "Same mission", Status: "identified",
		}}, nil
	}
	runBullseye = func(args ...string) (string, error) {
		return "ok\nids: T999\n", nil
	}
	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd": t.TempDir(), "name": "Same mission", "force": true,
	}
	res, err := s.handleTargetFile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := targetFileToolText(res)
	if !strings.Contains(text, "__TARGET_FILED__:T999") {
		t.Fatalf("force split want T999, got %q", text)
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
