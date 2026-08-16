// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/panecensus"
)

func t459Server(t *testing.T, defs []claudia.AgentDef, panes []panecensus.Pane) (*Server, *[]string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range defs {
		if d.WorkDir == "" {
			d.WorkDir = dir
		}
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	var mu sync.Mutex
	killed := []string{}
	s.SetPaneCensusIO(func() ([]panecensus.Pane, error) {
		mu.Lock()
		defer mu.Unlock()
		var live []panecensus.Pane
		dead := map[string]bool{}
		for _, id := range killed {
			dead[id] = true
		}
		for _, p := range panes {
			if !dead[p.ID] {
				live = append(live, p)
			}
		}
		return live, nil
	}, func(id string) error {
		mu.Lock()
		killed = append(killed, id)
		mu.Unlock()
		return nil
	})
	return s, &killed
}

func t459Pane(window, id, agent string, f panecensus.Flight) panecensus.Pane {
	return panecensus.Pane{Window: window, ID: id, AgentName: agent}.WithFlight(f)
}

func TestT459SweepReapsUnregisteredIdleOnly(t *testing.T) {
	panes := []panecensus.Pane{
		t459Pane("jv-live", "%live", "jv-live", panecensus.FlightIdle),
		t459Pane("orphan-idle", "%idle", "", panecensus.FlightIdle),
		t459Pane("orphan-busy", "%busy", "", panecensus.FlightInFlight),
		t459Pane("claudia-pool-0", "%p0", "", panecensus.FlightIdle),
		t459Pane("claudia-pool-1", "%p1", "", panecensus.FlightIdle),
		t459Pane("claudia-pool-2", "%p2", "", panecensus.FlightIdle),
	}
	s, killed := t459Server(t, []claudia.AgentDef{{Name: "jv-live", SessionID: "s1"}}, panes)
	n := s.SweepOrphanPanes()
	// idle orphan + one pool excess (bound 2) = 2
	if n != 2 {
		t.Fatalf("reaped %d, want 2 (idle orphan + pool excess); killed=%v", n, *killed)
	}
	got := map[string]bool{}
	for _, id := range *killed {
		got[id] = true
	}
	if !got["%idle"] {
		t.Fatal("idle orphan was not reaped")
	}
	if got["%busy"] {
		t.Fatal("mid-turn unregistered pane was reaped")
	}
	if got["%live"] {
		t.Fatal("registered pane was reaped")
	}
}

func TestT459AgentListNamesHostCost(t *testing.T) {
	s, _ := t459Server(t, []claudia.AgentDef{{Name: "jv-live", SessionID: "s1"}}, nil)
	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(res)
	if !strings.Contains(text, "host cost (est.)") || !strings.Contains(text, "🎯T459") {
		t.Fatalf("agent_list missing host cost: %s", text)
	}
}
