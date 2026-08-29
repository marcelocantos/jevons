// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"strings"
	"testing"
	"time"
)

// 🎯T561: a Claude context blow with weekly remaining stays on Claude.
func TestT561ContextRemintStaysOnClaudeWhenWeeklyRemains(t *testing.T) {
	th := DefaultThresholds()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	week := now.Add(4 * 24 * time.Hour)
	lim := DefaultWeeklyWindowSeconds
	pct := func(v float64) *float64 { return &v }
	claude := func(rem, used float64) Backend {
		return Backend{Provider: "claude", Status: StatusAvailable, Windows: []Window{{
			Name: WindowWeekly, RemainingPercent: pct(rem), UsedPercent: pct(used),
			ResetsAt: &week, LimitWindowSeconds: &lim,
		}}}
	}
	base := RemintArgs{SeatProvider: "claude", Known: true, Now: now, Thresholds: th}

	a := base
	a.Backend = claude(60, 40)
	p := ContextRemintPlan(a)
	if p.Mode != RemintSameProvider || p.Provider != "claude" {
		t.Fatalf("weekly ok → same-provider kill+start, got %+v", p)
	}
	adv := p.Advice("jevons-po")
	for _, want := range []string{"jevons_agent_kill(jevons-po)", `provider="claude"`, "do NOT jevons_agent_migrate"} {
		if !strings.Contains(adv, want) {
			t.Fatalf("advice lacks %q: %s", want, adv)
		}
	}

	a.Backend = claude(0, 100)
	if p := ContextRemintPlan(a); p.Mode != RemintMigrate || !strings.Contains(p.Reason, "exhausted") {
		t.Fatalf("weekly exhausted → migrate, got %+v", p)
	}

	a.Backend = Backend{Provider: "claude", Status: StatusUnavailable, Reason: "429 rate_limit"}
	if p := ContextRemintPlan(a); p.Mode != RemintMigrate {
		t.Fatalf("429 → migrate, got %+v", p)
	}

	u := base
	u.Known = false
	if p := ContextRemintPlan(u); p.Mode != RemintSameProvider || p.Provider != "claude" {
		t.Fatalf("unknown plan feed → stay, got %+v", p)
	}

	o := base
	o.Backend = claude(60, 40)
	o.OwnerAsked = true
	if p := ContextRemintPlan(o); p.Mode != RemintMigrate {
		t.Fatalf("owner asked → migrate, got %+v", p)
	}
}
