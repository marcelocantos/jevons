// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestDialMCPRoundTrip pins the real client against a real streamable-HTTP
// MCP server: initialize handshake, tools/list, tools/call with args.
func TestDialMCPRoundTrip(t *testing.T) {
	psrv := server.NewMCPServer("fake-provider", "0.0.1")
	psrv.AddTool(
		mcp.NewTool("echo",
			mcp.WithDescription("Echo text back"),
			mcp.WithString("text", mcp.Required()),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("echo:" + req.GetString("text", "")), nil
		},
	)
	ts := httptest.NewServer(server.NewStreamableHTTPServer(psrv, server.WithStateLess(true)))
	defer ts.Close()

	ctx := context.Background()
	c, err := DialMCP(ctx, ts.URL)
	if err != nil {
		t.Fatalf("DialMCP: %v", err)
	}
	defer c.Close()

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v, want one \"echo\"", tools)
	}

	res, err := c.CallTool(ctx, "echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("result content = %+v, want one text block", res.Content)
	}
	tc, ok := mcp.AsTextContent(res.Content[0])
	if !ok || tc.Text != "echo:hi" {
		t.Errorf("result = %+v, want text \"echo:hi\"", res.Content[0])
	}
}

// TestDialMCPRefusedEndpoint pins that a dead endpoint is an error, not a hang.
func TestDialMCPRefusedEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	url := ts.URL
	ts.Close() // now refused

	if _, err := DialMCP(context.Background(), url); err == nil {
		t.Fatal("DialMCP against a closed endpoint succeeded")
	}
}
