// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os/exec"
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

// 🎯T546 acceptance 3: the refuse must not break the blessed writers.
func TestBullseyeCommitAndTargetFileStillSucceed(t *testing.T) {
	if _, err := exec.LookPath("bullseye"); err != nil {
		t.Skip("bullseye not on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	gitInit(t, repo)
	open := exec.Command("bullseye", "open", "--cwd", repo, "--location", "in_repo")
	if out, err := open.CombinedOutput(); err != nil {
		t.Fatalf("bullseye open: %v\n%s", err, out)
	}
	track := exec.Command("bullseye", "commit", "--op", "track",
		"--cwd", repo,
		"--name", "T546 hermetic: bullseye commit still writes",
		"--acceptance", "CLI track succeeds on a fixture ledger")
	out, err := track.CombinedOutput()
	if err != nil {
		t.Fatalf("bullseye commit --op track: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "ids:") && !strings.Contains(text, "ok: true") {
		t.Fatalf("track output unexpected:\n%s", text)
	}

	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"cwd":        repo,
		"name":       "T546 hermetic: jevons_target_file still files",
		"acceptance": "MCP track path still allocates an id",
	}
	res, err := s.handleTargetFile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	got := targetFileToolText(res)
	if !strings.Contains(got, "__TARGET_FILED__:") {
		t.Fatalf("jevons_target_file failed:\n%s", got)
	}
	if strings.Contains(got, "bullseye commit failed") {
		t.Fatalf("jevons_target_file could not run bullseye:\n%s", got)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "init.defaultBranch=master", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
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
