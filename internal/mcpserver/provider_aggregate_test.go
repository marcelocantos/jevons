// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpgoserver "github.com/mark3labs/mcp-go/server"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/provider"
)

// TestProviderToolsAggregateIntoMCPSurface is the 🎯T27.4 acceptance
// oracle, end to end on real wire protocol: a provider process serving
// MCP over streamable HTTP has its tools re-exported — namespaced and
// attributed — on jevonsd's /mcp surface, and the aggregation follows
// provider enable/disable through config reload.
func TestProviderToolsAggregateIntoMCPSurface(t *testing.T) {
	// The "provider": a real MCP server on its own endpoint.
	psrv := mcpgoserver.NewMCPServer("testprov", "0.0.1")
	psrv.AddTool(
		mcp.NewTool("echo",
			mcp.WithDescription("Echo text back"),
			mcp.WithString("text", mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("echo:" + req.GetString("text", "")), nil
		},
	)
	provTS := httptest.NewServer(mcpgoserver.NewStreamableHTTPServer(psrv, mcpgoserver.WithStateLess(true)))
	defer provTS.Close()

	// The hub: a jevons MCP server exposing /mcp, as the daemon does.
	hub := mcpserver.New(t.TempDir(), nil, nil)
	mux := http.NewServeMux()
	hub.RegisterRoutes(mux)
	hubTS := httptest.NewServer(mux)
	defer hubTS.Close()

	// Desired-set plumbing exactly as cmd/jevonsd wires it: ConfigManager
	// → (SetDecls + Reconcile) on change; aggregator observes phases.
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeCfg := func(enable bool) {
		t.Helper()
		yaml := fmt.Sprintf(`
providers:
  - id: testprov
    transport: connect
    enable: %v
    params:
      url: %s
      mcp_url: %s
`, enable, provTS.URL, provTS.URL)
		if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeCfg(true)

	store, err := provider.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	agg := provider.NewAggregator(provider.AggregatorArgs{Sink: hub, RetryInterval: 20 * time.Millisecond})
	defer agg.Shutdown()
	lc := provider.NewLifecycle(provider.LifecycleArgs{
		OnPhase:       agg.HandlePhase,
		ProbeInterval: 20 * time.Millisecond,
	})
	defer lc.Shutdown(context.Background())

	mgr, err := provider.NewConfigManager(provider.ConfigManagerArgs{ConfigPath: cfgPath, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	agg.SetDecls(mgr.Desired())
	mgr.SetOnChange(func(decls []config.ProviderDecl) {
		agg.SetDecls(decls)
		if err := lc.Reconcile(decls); err != nil {
			t.Errorf("reconcile: %v", err)
		}
	})
	if err := lc.Reconcile(mgr.Desired()); err != nil {
		t.Fatal(err)
	}

	// Consume the hub surface the way the overseer and workers do: a real
	// MCP client against /mcp.
	ctx := context.Background()
	hubClient, err := provider.DialMCP(ctx, hubTS.URL+"/mcp")
	if err != nil {
		t.Fatalf("dial hub /mcp: %v", err)
	}
	defer hubClient.Close()

	hasTool := func(name string) func() bool {
		return func() bool {
			tools, err := hubClient.ListTools(ctx)
			if err != nil {
				return false
			}
			for _, tool := range tools {
				if tool.Name == name {
					return true
				}
			}
			return false
		}
	}
	waitFor := func(msg string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal(msg)
	}

	// 1. Enabled provider → namespaced tool appears on /mcp.
	waitFor("testprov__echo never appeared on /mcp", hasTool("testprov__echo"))

	// Attribution rides the re-exported description.
	tools, err := hubClient.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Name == "testprov__echo" && !strings.HasPrefix(tool.Description, "[provider testprov] ") {
			t.Errorf("tool not attributed: %q", tool.Description)
		}
	}

	// 2. Calling through the hub reaches the provider and returns its result.
	res, err := hubClient.CallTool(ctx, "testprov__echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("call testprov__echo via hub: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("result content = %+v", res.Content)
	}
	if tc, ok := mcp.AsTextContent(res.Content[0]); !ok || tc.Text != "echo:hi" {
		t.Errorf("hub call result = %+v, want \"echo:hi\"", res.Content[0])
	}

	// 3. Owner disables the provider in config; reload withdraws the tool.
	writeCfg(false)
	if err := mgr.Reload(); err != nil {
		t.Fatal(err)
	}
	waitFor("testprov__echo still on /mcp after disable+reload", func() bool { return !hasTool("testprov__echo")() })

	// 4. Re-enable; reload brings it back.
	writeCfg(true)
	if err := mgr.Reload(); err != nil {
		t.Fatal(err)
	}
	waitFor("testprov__echo did not reappear after enable+reload", hasTool("testprov__echo"))
}
