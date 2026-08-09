// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/research"
)

const researchBullseyeFixture = `
schema_version: 5
targets:
  T356:
    name: Ambient research agents
    status: identified
  T1:
    name: Locked down
    status: achieved
`

// newResearchTestServer wires a research agent over temp dirs, with a workdir
// carrying a bullseye ledger so a context cycle has something to find.
func newResearchTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	state := t.TempDir()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "bullseye.yaml"), []byte(researchBullseyeFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(work, nil, nil)
	agent, err := research.New(research.Args{StateDir: state, Workdir: work})
	if err != nil {
		t.Fatalf("research.New: %v", err)
	}
	s.SetResearchAgent(agent)
	return s, state
}

func researchCall(t *testing.T, fn func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) string {
	t.Helper()
	res, err := fn(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	text := ideaToolText(res)
	if res.IsError {
		t.Fatalf("tool error: %s", text)
	}
	return text
}

func TestResearchCycleListReadMCP(t *testing.T) {
	s, _ := newResearchTestServer(t)

	text := researchCall(t, s.handleResearchCycle, map[string]any{"mode": "context"})
	if !strings.Contains(text, "context/frontier rev 1") {
		t.Fatalf("cycle should report the new note revision: %s", text)
	}

	listText := researchCall(t, s.handleResearchList, map[string]any{})
	if !strings.Contains(listText, "context-frontier") || !strings.Contains(listText, ".md") {
		t.Fatalf("list must expose the note id and its markdown path: %s", listText)
	}

	readText := researchCall(t, s.handleResearchRead, map[string]any{"id": "context/frontier"})
	if !strings.Contains(readText, "Current findings") || !strings.Contains(readText, "1 identified") {
		t.Fatalf("read should render the note: %s", readText)
	}

	// A second identical cycle finds nothing new and says so.
	quiet := researchCall(t, s.handleResearchCycle, map[string]any{"mode": "context"})
	if !strings.Contains(quiet, "nothing new") {
		t.Fatalf("unchanged context should report quiet: %s", quiet)
	}
}

func TestResearchCycleRejectsUnknownMode(t *testing.T) {
	s, _ := newResearchTestServer(t)
	res, err := s.handleResearchCycle(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"mode": "sideways"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(ideaToolText(res), "unknown mode") {
		t.Fatalf("want a mode error, got %v", ideaToolText(res))
	}
}

func TestResearchReadMissingNote(t *testing.T) {
	s, _ := newResearchTestServer(t)
	res, err := s.handleResearchRead(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"id": "repo/nope"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("missing note should be an error, got %s", ideaToolText(res))
	}
}

func TestResearchConfigureSubscribesFeedAndReportsStatus(t *testing.T) {
	s, state := newResearchTestServer(t)

	text := researchCall(t, s.handleResearchConfigure, map[string]any{
		"feed_enabled":   true,
		"add_feed_name":  "model-news",
		"add_feed_url":   "https://news.example.com/rss",
		"interval_sec":   float64(600),
		"lookback_hours": float64(24),
		"updated_by":     "jevons",
	})
	if !strings.Contains(text, "model-news") || !strings.Contains(text, "feed/model-news") {
		t.Fatalf("configure should echo the subscription: %s", text)
	}

	cfg, err := research.LoadConfig(state)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Feeds) != 1 || !cfg.FeedEnabled || cfg.IntervalSec != 600 || cfg.LookbackHours != 24 {
		t.Fatalf("durable config not applied: %+v", cfg)
	}
	if !cfg.HostAllowed("https://news.example.com/rss") {
		t.Fatalf("subscribing must allow the host: %+v", cfg.AllowedHosts)
	}

	status := researchCall(t, s.handleResearchStatus, map[string]any{})
	if !strings.Contains(status, "interval_sec=600") || !strings.Contains(status, "model-news") {
		t.Fatalf("status should reflect config: %s", status)
	}

	// Feed poll with no reachable fixture must degrade to a skip, not an error.
	poll := researchCall(t, s.handleResearchCycle, map[string]any{"mode": "feed"})
	if !strings.Contains(poll, "feeds polled: 1") || !strings.Contains(poll, "skipped:") {
		t.Fatalf("unreachable feed should be reported as a skip: %s", poll)
	}

	removed := researchCall(t, s.handleResearchConfigure, map[string]any{"remove_feed": "model-news"})
	if strings.Contains(removed, "https://news.example.com/rss") {
		t.Fatalf("unsubscribe should drop the feed: %s", removed)
	}
}

func TestResearchConfigureRequiresFeedURLWithName(t *testing.T) {
	s, _ := newResearchTestServer(t)
	res, err := s.handleResearchConfigure(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"add_feed_name": "model-news"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(ideaToolText(res), "add_feed_url") {
		t.Fatalf("want a url-required error, got %s", ideaToolText(res))
	}
}

func TestResearchToolsRefuseWhenUnconfigured(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	for name, fn := range map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){
		"cycle":     s.handleResearchCycle,
		"list":      s.handleResearchList,
		"read":      s.handleResearchRead,
		"configure": s.handleResearchConfigure,
		"status":    s.handleResearchStatus,
	} {
		res, err := fn(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"id": "x"}}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !res.IsError || !strings.Contains(ideaToolText(res), "not configured") {
			t.Fatalf("%s should refuse without an agent, got %s", name, ideaToolText(res))
		}
	}
}
