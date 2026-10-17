// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/workers"
	"github.com/mark3labs/mcp-go/mcp"
)

// The real jwork handler constructs Claudia's Task, without a task stub or a
// replacement engine. Transport removal must surface to the caller and settle
// the worker row, even with an ordinary non-cancelled request context.
func TestJworkRemovedTaskFailsVisiblyAndSettlesWorker(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	t.Setenv("CLAUDIA_BROKER_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	for _, key := range []string{"CLAUDE_BIN", "CODEX_BIN", "GROK_BIN", "CURSOR_BIN"} {
		t.Setenv(key, "/nonexistent/removed-task-cli")
	}
	for _, provider := range []string{"", "claude", "codex", "grok", "cursor"} {
		t.Run(provider, func(t *testing.T) {
			tr, err := openTestWorkers(t)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { tr.Close() })
			eng, err := openTestDoit(t)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { eng.Close() })
			s := &Server{workerWD: t.TempDir()}
			s.SetWorkersTracker(tr)
			s.SetDoitEngine(eng)
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"text": "Summarise the task description.", "provider": provider}
			result, err := s.handleJwork(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.Contains(extractText(result), claudia.ErrTaskCLIRemoved.Error()) {
				t.Fatalf("jwork did not expose the removal: %+v", result)
			}
			rows, err := tr.Store.List("", 10)
			if err != nil || len(rows) != 1 {
				t.Fatalf("worker rows=%+v, %v", rows, err)
			}
			if rows[0].Status != workers.StatusFailed || !strings.Contains(rows[0].Outcome, claudia.ErrTaskCLIRemoved.Error()) {
				t.Fatalf("removed task left misleading worker state: %+v", rows[0])
			}
		})
	}
}
