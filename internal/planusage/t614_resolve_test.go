// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// Claudia Resolve ranks published dests by plan slack (claudia v0.32+); it
// no longer implements the owner's Claude-first rule. That rule is
// ClaudeFirst here (🎯T583), consulted by mintProviderPick before Resolve.
// These oracles pin what each side promises.

func TestT614ResolveSkipsHotClaude(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	cands := []DestCand{
		{Provider: "claude", Backend: Backend{
			Provider: "claude", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(20), UsedPercent: pct(80),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}},
		{Provider: "grok", Backend: Backend{
			Provider: "grok", Status: StatusUnavailable, Reason: "unpublished",
		}},
	}
	got, err := claudia.Resolve(context.Background(), claudia.ModelPredicates{
		Mode:           claudia.CapabilitySession,
		PreferPlan:     true,
		PreferProvider: claudia.ProviderClaude,
		Now:            now,
		Usage:          backendsToPlanUsage(cands),
	})
	// With Claude hot and Grok unpublished there is no green published
	// dest: Resolve refuses (a named tie among unpublished catalog rows)
	// rather than landing on hot Claude. A refusal is the accepted answer;
	// a pick must not be Claude.
	if err == nil && got.Provider == claudia.ProviderClaude {
		t.Fatalf("hot Claude must not be chosen: %+v", got)
	}
}

func TestT614ResolveClaudeFirstWithHeadroom(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	week := now.Add(3*24*time.Hour + 12*time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	cands := []DestCand{
		{Provider: "claude", Backend: Backend{
			Provider: "claude", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(50), UsedPercent: pct(50),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}},
		{Provider: "grok", Backend: Backend{
			Provider: "grok", Status: StatusAvailable,
			Windows: []Window{{
				Name: WindowWeekly, RemainingPercent: pct(87), UsedPercent: pct(13),
				ResetsAt: &week, LimitWindowSeconds: &lim,
			}},
		}},
	}
	// The owner rule: Claude with headroom wins even over a fresher Grok.
	cf := ClaudeFirst(cands, now, DefaultThresholds())
	if !cf.OK {
		t.Fatalf("claude-first with headroom passed Claude over: %+v", cf)
	}
	if cf.Headroom == nil || *cf.Headroom != 50 {
		t.Fatalf("claude-first headroom = %v, want 50", cf.Headroom)
	}
	// Resolve itself still has a published dest to offer for the omit
	// path that runs after ClaudeFirst declines.
	got, err := claudia.Resolve(context.Background(), claudia.ModelPredicates{
		Mode:       claudia.CapabilitySession,
		PreferPlan: true,
		Now:        now,
		Usage:      backendsToPlanUsage(cands),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != claudia.ProviderClaude && got.Provider != claudia.ProviderGrok {
		t.Fatalf("resolve left the published set: %+v", got)
	}
}
