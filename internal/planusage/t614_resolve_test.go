// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

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
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider == claudia.ProviderClaude {
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
	got, err := claudia.Resolve(context.Background(), claudia.ModelPredicates{
		Mode:           claudia.CapabilitySession,
		PreferPlan:     true,
		PreferProvider: claudia.ProviderClaude,
		Now:            now,
		Usage:          backendsToPlanUsage(cands),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != claudia.ProviderClaude {
		t.Fatalf("claude-first with headroom: %+v", got)
	}
}
