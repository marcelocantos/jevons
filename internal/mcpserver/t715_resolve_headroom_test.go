// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"

	"github.com/marcelocantos/jevons/internal/capacity"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func t715Caps() map[string]int {
	return map[string]int{"claude": 12, "codex": 6, "grok": 12}
}

func t715Server(t *testing.T, load map[string]int) *Server {
	t.Helper()
	now := time.Date(2026, 9, 21, 0, 50, 0, 0, time.UTC)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))
	s.SetProviderSoftCaps(t715Caps())
	s.SetPlanUsageSource(func() planusage.Snapshot {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{
			t583Weekly("claude", 56, now),
			t583Weekly("grok", 87, now),
			t583Weekly("codex", 70, now),
		}}
	})
	fill := func(prov claudia.Provider, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			if err := reg.Register(claudia.AgentDef{
				Name:      fmt.Sprintf("fill-%s-%d", prov, i),
				WorkDir:   t.TempDir(),
				SessionID: fmt.Sprintf("%s-%d", prov, i),
				Provider:  prov,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	fill(claudia.ProviderClaude, load["claude"])
	fill(claudia.ProviderGrok, load["grok"])
	fill(claudia.Provider("codex"), load["codex"])
	copied := map[string]int{}
	for k, v := range load {
		copied[k] = v
	}
	s.SetCapacityGovernor(capacity.NewGovernor(capacity.GovernorArgs{
		Snapshot: func() capacity.Snapshot {
			n := 0
			for _, v := range copied {
				n += v
			}
			return capacity.Snapshot{
				MaxSessions:      20,
				ActiveSessions:   n,
				ProviderLoad:     copied,
				ProviderSoftCaps: t715Caps(),
			}
		},
	}))
	return s
}

// 🎯T715 product path: claude 12/12 grok 0/12, omit-provider code_implement
// mint resolves to grok and dest-aware admit lets it through.
func TestT715OmitProviderMintsGrokWhenClaudeAtCap(t *testing.T) {
	s := t715Server(t, map[string]int{"claude": 12, "grok": 0, "codex": 0})
	pick := s.mintProviderPick("", "", false, "code_implement", string(claudia.PurposeWork), "jv-t715-omit", false)
	if pick.Provider != cost.HarnessGrok {
		t.Fatalf("omit mint = %+v, want grok (claude at session cap skipped)", pick)
	}
	if blocked := s.checkDestSpawnAllowed("work", "jv-t715-omit", pick.Provider); blocked != nil {
		t.Fatalf("grok dest refused: %s", toolText(blocked))
	}
	if blocked := s.checkHostSpawnAllowed("work", "jv-t715-omit"); blocked != nil {
		t.Fatalf("dest-unaware early gate refused a pane while grok has room: %s", toolText(blocked))
	}
	def, _, note, err := s.stitchAgentStart(
		"jv-t715-omit", t.TempDir(), "", "", "code_implement",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if cli.PlanProvider(def.Provider) != claudia.ProviderGrok {
		t.Fatalf("omit spawn minted %q, want grok; note=%q", def.Provider, note)
	}
}

// 🎯T715 clause 2: explicit exhausted dest is refused and names dests with
// headroom. handleAgentStart is the owner-visible refuse (before Launch).
func TestT715ExplicitExhaustedDestRefusesNamingHeadroom(t *testing.T) {
	s := t715Server(t, map[string]int{"claude": 12, "grok": 0, "codex": 0})
	pick := s.mintProviderPick("claude", "", false, "code_implement", string(claudia.PurposeWork), "jv-t715-explicit", false)
	if pick.Provider != cost.HarnessClaude {
		t.Fatalf("explicit claude dropped: %+v", pick)
	}
	blocked := s.checkDestSpawnAllowed("work", "jv-t715-explicit", pick.Provider)
	if blocked == nil {
		t.Fatal("explicit claude at cap was admitted")
	}
	got := toolText(blocked)
	if !strings.Contains(got, "claude") || !strings.Contains(got, "dests with headroom") || !strings.Contains(got, "grok") {
		t.Fatalf("refuse must name claude and dests with headroom: %s", got)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":     "jv-t715-explicit-start",
		"workdir":  t.TempDir(),
		"purpose":  "work",
		"provider": "claude",
	}
	result, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError {
		t.Fatal("handleAgentStart must refuse explicit claude at cap before Launch")
	}
	startGot := toolText(result)
	if !strings.Contains(startGot, "dests with headroom") || !strings.Contains(startGot, "grok") {
		t.Fatalf("handleAgentStart refuse: %s", startGot)
	}
}

func TestT715AllDestsAtCapRefusesOmitAndExplicit(t *testing.T) {
	s := t715Server(t, map[string]int{"claude": 12, "grok": 12, "codex": 6})
	omit := s.mintProviderPick("", "", false, "code_implement", string(claudia.PurposeWork), "jv-t715-omit-full", false)
	if strings.TrimSpace(omit.Provider) != "" {
		t.Fatalf("omit mint landed on %q when every dest is at cap", omit.Provider)
	}
	exp := s.mintProviderPick("claude", "", false, "code_implement", string(claudia.PurposeWork), "jv-t715-exp-full", false)
	if exp.Provider != cost.HarnessClaude {
		t.Fatalf("explicit claude dropped at all-cap: %+v", exp)
	}
	blocked := s.checkDestSpawnAllowed("work", "jv-t715-exp-full", exp.Provider)
	if blocked == nil {
		t.Fatal("explicit claude admitted when every dest is at cap")
	}
	if !strings.Contains(toolText(blocked), "dests with headroom: none") {
		t.Fatalf("all-cap refuse: %s", toolText(blocked))
	}
	if blocked := s.checkHostSpawnAllowed("work", "jv-t715-omit-full"); blocked == nil {
		t.Fatal("dest-unaware early gate admitted when every dest is at cap")
	}
	_, _, _, err := s.stitchAgentStart(
		"jv-t715-omit-full", t.TempDir(), "", "", "code_implement",
		"jevons-po", claudia.PurposeWork, "", "",
	)
	if err == nil {
		t.Fatal("omit stitch minted a dest when every configured dest is at cap")
	}
}
