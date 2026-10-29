// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// A jevons_land_hold call without active reads the hold; it used to clear
// the fleet NoMerge hold because the missing bool read as false (2026-10-27).
func TestLandHoldWithoutActiveReadsInsteadOfClearing(t *testing.T) {
	s := &Server{}
	if err := s.OpenLandHold(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	call := func(args map[string]any) string {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = args
		res, err := s.handleLandHold(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return toolText(res)
	}
	call(map[string]any{"scope": "fleet", "active": true, "hold": "no-merge", "actor": "owner"})
	if got := call(map[string]any{"scope": "fleet"}); !strings.Contains(got, "in effect") {
		t.Fatalf("read reported %q", got)
	}
	if !s.landHoldStore().EffectiveHold("").NoMerge {
		t.Fatal("a read cleared the fleet hold")
	}
	call(map[string]any{"scope": "fleet", "active": false})
	if s.landHoldStore().EffectiveHold("").Active() {
		t.Fatal("active=false did not clear")
	}
}
