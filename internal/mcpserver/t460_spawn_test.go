// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/capacity"
)

func t460gib(g float64) int64 { return int64(g * float64(int64(1)<<30)) }

func meltedSpawnSnap() capacity.Snapshot {
	return capacity.Snapshot{
		HostLoad1:          247,
		HostCores:          16,
		HostSwapUsedBytes:  t460gib(36.3),
		HostSwapTotalBytes: t460gib(37.9),
		HostSource:         "test fixture (2026-08-15)",
	}
}

func idleSpawnSnap() capacity.Snapshot {
	return capacity.Snapshot{
		HostLoad1:          3.2,
		HostCores:          16,
		HostSwapUsedBytes:  t460gib(4),
		HostSwapTotalBytes: t460gib(37.9),
		HostSource:         "test fixture (idle)",
	}
}

func TestT460AgentStartRefusedAtCriticalHostPressure(t *testing.T) {
	s := &Server{capacityGov: capacity.NewGovernor(capacity.GovernorArgs{
		Snapshot: meltedSpawnSnap,
	})}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "jv-t460-auto",
		"workdir": "/tmp/work",
		"purpose": "work",
	}
	result, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected tool error at critical host pressure")
	}
	got := toolText(result)
	if !strings.Contains(got, "host pressure") || !strings.Contains(got, "🎯T460") {
		t.Fatalf("error %q does not name host pressure", got)
	}
}

func TestT460IdleHostStillReachesRegistry(t *testing.T) {
	s := &Server{capacityGov: capacity.NewGovernor(capacity.GovernorArgs{
		Snapshot: idleSpawnSnap,
	})}
	if blocked := s.checkHostSpawnAllowed("work", "jv-t460-auto"); blocked != nil {
		t.Fatalf("idle host refused spawn: %s", toolText(blocked))
	}
}

func TestT460OverseerStartNotBlockedOnMeltedHost(t *testing.T) {
	s := &Server{capacityGov: capacity.NewGovernor(capacity.GovernorArgs{
		Snapshot: meltedSpawnSnap,
	})}
	if blocked := s.checkHostSpawnAllowed("overseer", "jevons"); blocked != nil {
		t.Fatalf("overseer seat refused on melted host: %s", toolText(blocked))
	}
}
