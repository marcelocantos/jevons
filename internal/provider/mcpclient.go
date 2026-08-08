// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPToolClient is the hub's client-side view of one provider MCP server
// (🎯T27.4). internal/mcpserver is the producer half of jevonsd's MCP
// story; this is the net-new consumer half that lets the hub aggregate
// tools other processes serve.
type MCPToolClient interface {
	ListTools(ctx context.Context) ([]mcp.Tool, error)
	CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error)
	Close() error
}

// MCPDialer opens an MCPToolClient on a provider's declared MCP endpoint.
// Production uses DialMCP; tests inject fakes.
type MCPDialer func(ctx context.Context, endpoint string) (MCPToolClient, error)

// MCPDialTimeout bounds the transport start + initialize handshake for
// one dial attempt. Tool calls themselves ride the caller's context.
const MCPDialTimeout = 10 * time.Second

// DialMCP connects to a provider MCP endpoint over streamable HTTP and
// completes the initialize handshake. The declared endpoint is
// params.mcp_url on the provider declaration (config.ProviderDecl.MCPURL).
func DialMCP(ctx context.Context, endpoint string) (MCPToolClient, error) {
	c, err := mcpclient.NewStreamableHttpClient(endpoint)
	if err != nil {
		return nil, fmt.Errorf("mcp dial %s: %w", endpoint, err)
	}
	hctx, cancel := context.WithTimeout(ctx, MCPDialTimeout)
	defer cancel()
	if err := c.Start(hctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp start %s: %w", endpoint, err)
	}
	var init mcp.InitializeRequest
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "jevonsd", Version: "1.0.0"}
	if _, err := c.Initialize(hctx, init); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp initialize %s: %w", endpoint, err)
	}
	return &mcpConn{c: c}, nil
}

type mcpConn struct{ c *mcpclient.Client }

func (m *mcpConn) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	res, err := m.c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	return res.Tools, nil
}

func (m *mcpConn) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	return m.c.CallTool(ctx, req)
}

func (m *mcpConn) Close() error { return m.c.Close() }
