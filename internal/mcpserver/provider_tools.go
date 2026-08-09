// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// AddProviderTool re-exports one aggregated provider tool onto the /mcp
// surface (🎯T27.4). The provider.Aggregator hands tools in already
// namespaced ("{provider}__{name}") and attributed; this is the ToolSink
// half, kept structural so internal/provider never imports this package.
func (s *Server) AddProviderTool(t mcp.Tool, h func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	s.mcpSrv.AddTool(t, h)
}

// RemoveProviderTools withdraws aggregated provider tools when their
// provider leaves Running or is disabled/removed (🎯T27.4).
func (s *Server) RemoveProviderTools(names ...string) {
	s.mcpSrv.DeleteTools(names...)
}
