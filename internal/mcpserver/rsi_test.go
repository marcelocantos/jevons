// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/rsi"
)

func TestHandleRSICycle(t *testing.T) {
	state := t.TempDir()
	logPath := eventlog.DefaultPath(state)
	j, err := eventlog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := j.Append(eventlog.Event{
			TS:        time.Now().UTC().Format(time.RFC3339Nano),
			Source:    "server",
			Level:     "error",
			Msg:       "call failed",
			Component: "mcp",
			Decision:  "tool",
			Fields:    map[string]any{"outcome": "error"},
			Corr:      "x",
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = j.Close()

	mintCwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(mintCwd, "bullseye.yaml"), []byte("schema_version: 1\ntargets: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var n int
	loop, err := rsi.NewLoop(rsi.LoopArgs{
		StateDir:     state,
		MintCwd:      mintCwd,
		EventLogPath: logPath,
		Interval:     -1,
		Filer: filerFunc(func(a rsi.FileArgs) (string, error) {
			n++
			if a.Name == "" || len(a.Acceptance) == 0 {
				t.Errorf("incomplete file args: %+v", a)
			}
			return "T920", nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	s := New(t.TempDir(), nil, nil)
	s.SetRSILoop(loop)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleRSICycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := rsiToolText(res)
	if !strings.Contains(text, "filed") || !strings.Contains(text, "T920") {
		t.Fatalf("want filed T920 in response, got %q", text)
	}
	if n != 1 {
		t.Fatalf("filer called %d times", n)
	}
}

func TestHandleRSICycleNotConfigured(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}
	res, err := s.handleRSICycle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := rsiToolText(res)
	if !strings.Contains(strings.ToLower(text), "not configured") {
		t.Fatalf("want not configured, got %q", text)
	}
}

type filerFunc func(rsi.FileArgs) (string, error)

func (f filerFunc) File(a rsi.FileArgs) (string, error) { return f(a) }

func rsiToolText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	// IsError tools still put text in Content on mark3labs
	return b.String()
}
