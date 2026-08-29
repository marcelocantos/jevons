// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/hostload"
)

// jevons_capacity_status reports the kernel memory level as its own
// dimension and admits spawns on a scarred-swap host (🎯T573).
func TestT573CapacityStatusReportsMemoryLevelAndAdmits(t *testing.T) {
	scarred := func() capacity.Snapshot {
		var snap capacity.Snapshot
		applyHostLoad(&snap, hostload.Sample{
			Load1: 6, Cores: 16,
			SwapUsedBytes: t460gib(16.15), SwapTotalBytes: t460gib(17),
			MemoryFreePercent: 75, MemoryPressure: hostload.PressureNormal,
			Source: "test fixture (2026-08-29)",
		})
		return snap
	}
	s := &Server{capacityGov: capacity.NewGovernor(capacity.GovernorArgs{Snapshot: scarred})}
	result, err := s.handleCapacityStatus(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := toolText(result)
	for _, want := range []string{"memory grind: headroom", "memory 75% free, pressure normal", "advisory swap", "new worker panes admitted"} {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q:\n%s", want, got)
		}
	}
}
