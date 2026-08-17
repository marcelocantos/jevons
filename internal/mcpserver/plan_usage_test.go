// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestHandlePlanUsagePaintsTicker(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	eighty := 83.0
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{
			Backends: []planusage.Backend{
				{
					Provider: "claude",
					Status:   planusage.StatusUnavailable,
					Reason:   "Claude usage HTTP 429: rate_limit_error",
				},
				{
					Provider: "codex",
					Status:   planusage.StatusAvailable,
					Windows: []planusage.Window{{
						Name: planusage.WindowWeekly, RemainingPercent: &eighty,
					}},
				},
			},
		}
	})
	res, err := s.handlePlanUsage(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(res)
	if !strings.Contains(text, "EXHAUSTED") || !strings.Contains(text, "weekly 83%") {
		t.Fatalf("tool text:\n%s", text)
	}
}
