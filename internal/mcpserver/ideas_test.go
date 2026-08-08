// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestIdeaCaptureListTriageMCP(t *testing.T) {
	state := t.TempDir()
	s := New(state, nil, nil)
	s.SetIdeaStateDir(state)

	capRes, err := s.handleIdeaCapture(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"text":   "spark from MCP: no scrollback evaporation",
				"source": "mcp",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if capRes.IsError {
		t.Fatalf("capture error: %v", capRes.Content)
	}
	text := ideaToolText(capRes)
	if !strings.Contains(text, "Captured idea") {
		t.Fatalf("capture text: %s", text)
	}
	id := ""
	for _, part := range strings.Fields(text) {
		if strings.HasPrefix(part, "idea-") {
			id = part
			break
		}
	}
	if id == "" {
		t.Fatalf("no idea id in %q", text)
	}

	listRes, err := s.handleIdeaList(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	listText := ideaToolText(listRes)
	if !strings.Contains(listText, id) {
		t.Fatalf("list missing id: %s", listText)
	}

	triRes, err := s.handleIdeaTriage(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"id":          id,
				"disposition": "park",
				"note":        "needs design",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	triText := ideaToolText(triRes)
	if !strings.Contains(triText, "park") {
		t.Fatalf("triage: %s", triText)
	}
}

func ideaToolText(res *mcp.CallToolResult) string {
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
