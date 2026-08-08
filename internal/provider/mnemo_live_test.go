// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider_test

// Optional live probe: when a real mnemo is listening (default :19419),
// DialMCP lists tools and proves the aggregator transport works against
// the first live provider. Skips cleanly when mnemo is down so hermetic
// CI stays green. Enable with a running mnemo (no extra env required).

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/provider"
)

func TestLiveMnemoMCPDial(t *testing.T) {
	const endpoint = "http://127.0.0.1:19419/mcp"
	// Cheap liveness: /health must answer.
	hctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, "http://127.0.0.1:19419/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("mnemo not reachable at :19419: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		t.Skipf("mnemo /health status %d", resp.StatusCode)
	}

	ctx, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	cli, err := provider.DialMCP(ctx, endpoint)
	if err != nil {
		t.Fatalf("DialMCP mnemo: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	tools, err := cli.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("mnemo MCP returned zero tools")
	}
	// At least one tool name should start with mnemo_ (product surface).
	found := false
	for _, tool := range tools {
		if len(tool.Name) >= 5 && tool.Name[:5] == "mnemo" {
			found = true
			break
		}
	}
	if !found {
		// Still pass if tools exist — naming is soft — but log.
		t.Logf("mnemo tools present (%d) but none named mnemo*: first=%q", len(tools), tools[0].Name)
	}
}
