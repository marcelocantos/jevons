// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// This exercises the shipped query against real SQLite/sqldeep. Socket
// replication is covered separately; this test makes no transport claim.
func TestSubscribedProjectionQuery(t *testing.T) {
	db, err := sqlpipe.OpenDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := controlplane.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS claudia_plan_usage(id INTEGER PRIMARY KEY, provider TEXT, status TEXT, reason TEXT, attempted_at TEXT, success_at TEXT, next_attempt_at TEXT, reading_json TEXT);
 CREATE TABLE IF NOT EXISTS claudia_plan_usage_history(id INTEGER PRIMARY KEY, provider TEXT, observed_at TEXT, reading_json TEXT);`); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(projectionSchema); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	observation := now.Add(-time.Minute).Format(time.RFC3339)
	reset := now.Add(24 * time.Hour).Format(time.RFC3339)
	reading, _ := json.Marshal(map[string]any{"provider": "claude", "status": "available", "fetched_at": observation, "windows": []any{map[string]any{"name": "weekly", "remaining_percent": 41, "used_percent": 59, "resets_at": reset, "limit_window": int64(7 * 24 * time.Hour), "band": "ahead", "pressure": 0.5}}})
	if err := db.Exec("INSERT INTO claudia_plan_usage(provider,status,reason,attempted_at,success_at,next_attempt_at,reading_json) VALUES (?,?,?,?,?,?,?)", "claude", "available", "", observation, observation, now.Add(time.Minute).Format(time.RFC3339), string(reading)); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO claudia_plan_usage_history(provider,observed_at,reading_json) VALUES (?,?,?)", "claude", observation, string(reading)); err != nil {
		t.Fatal(err)
	}
	p := &Projection{}
	read := func() Snapshot {
		t.Helper()
		qr, err := db.Query(ProjectionQuery)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.accept(qr); err != nil {
			t.Fatalf("accept: %v rows=%#v", err, qr.Rows)
		}
		return p.Snapshot()
	}
	snap := read()
	if snap.Pending || len(snap.Backends) != 1 {
		t.Fatalf("snapshot: %+v", snap)
	}
	be := snap.Backends[0]
	if be.Status != StatusAvailable || len(be.Windows) != 1 || *be.Windows[0].RemainingPercent != 41 || be.Windows[0].Band != "ahead" || len(be.Windows[0].History) != 1 {
		t.Fatalf("backend: %+v payload=%s", be, p.JSON())
	}
	if err := db.Exec("INSERT INTO jv_plan_overrides(provider,override_json) VALUES (?,?)", "claude", `{"band":"ok","reason":"owner pin","set_at":"2026-01-01T00:00:00Z"}`); err != nil {
		t.Fatal(err)
	}
	be = read().Backends[0]
	if be.Override == nil || be.Windows[0].Band != "ok" || be.Windows[0].History[0].Band != "ok" {
		t.Fatalf("override: %s", p.JSON())
	}
	if err := db.Exec("UPDATE claudia_plan_usage SET status='unavailable',reason='429',attempted_at=?", now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	be = read().Backends[0]
	if be.Available() || !be.Stale || be.Windows[0].Band != "" || *be.Windows[0].RemainingPercent != 41 || !be.FetchedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("failure lost last good or false green: %s", p.JSON())
	}
}

func TestSubscribedProjectionSpend(t *testing.T) {
	db, err := sqlpipe.OpenDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := controlplane.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(projectionSchema); err != nil {
		t.Fatal(err)
	}
	const money = `"used":{"amount_minor":7284,"exponent":2,"currency":" aud "},"limit":{"amount_minor":10000,"exponent":2,"currency":"AUD"}`
	for _, tc := range []struct {
		name, provider, spend, override string
		used                            int
		wantSpend, unavailable, reached bool
	}{
		{name: "enabled", provider: "claude", spend: `{"enabled":true,` + money + `}`, used: 100, wantSpend: true},
		{name: "disabled", provider: "claude", spend: `{"enabled":false,` + money + `}`, used: 100},
		{name: "disabled but cap reached", provider: "claude", spend: `{"enabled":false,"limit_reached":true,` + money + `}`, used: 100, wantSpend: true, reached: true},
		{name: "missing spend", provider: "claude", used: 100, wantSpend: true, unavailable: true},
		{name: "missing money", provider: "claude", spend: `{"enabled":true}`, used: 100, wantSpend: true, unavailable: true},
		{name: "non AUD", provider: "claude", spend: `{"enabled":true,"limit_reached":true,"used":{"amount_minor":100,"exponent":2,"currency":"USD"}}`, used: 100, wantSpend: true, unavailable: true, reached: true},
		{name: "below cap", provider: "claude", spend: `{"enabled":true,` + money + `}`, used: 99},
		{name: "owner override", provider: "claude", spend: `{"enabled":true,` + money + `}`, used: 100, wantSpend: true, override: "ok"},
		{name: "codex", provider: "codex", spend: `{"enabled":true,` + money + `}`, used: 100, wantSpend: true},
		{name: "grok", provider: "grok", spend: `{"enabled":true,` + money + `}`, used: 100, wantSpend: true},
		{name: "cursor never money", provider: "cursor", spend: `{"enabled":true,` + money + `}`, used: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := db.Exec("DELETE FROM claudia_plan_usage; DELETE FROM jv_plan_overrides"); err != nil {
				t.Fatal(err)
			}
			reading := map[string]any{"windows": []any{map[string]any{"name": "session", "used_percent": tc.used, "band": "locked"}}}
			if tc.spend != "" {
				reading["spend"] = json.RawMessage(tc.spend)
			}
			raw, err := json.Marshal(reading)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Format(time.RFC3339)
			if err := db.Exec("INSERT INTO claudia_plan_usage(provider,status,reason,attempted_at,success_at,next_attempt_at,reading_json) VALUES (?,'available','',?,?,?,?)", tc.provider, now, now, now, string(raw)); err != nil {
				t.Fatal(err)
			}
			if tc.override != "" {
				if err := db.Exec("INSERT INTO jv_plan_overrides(provider,override_json) VALUES (?,?)", tc.provider, fmt.Sprintf(`{"band":%q}`, tc.override)); err != nil {
					t.Fatal(err)
				}
			}
			qr, err := db.Query(ProjectionQuery)
			if err != nil {
				t.Fatal(err)
			}
			p := &Projection{}
			if err := p.accept(qr); err != nil {
				t.Fatal(err)
			}
			be := p.Snapshot().Backends[0]
			if be.Windows[0].Band != "ok" {
				t.Fatalf("session locked must retain legacy neutral paint: %s", p.JSON())
			}
			sp := be.Spend
			if (sp != nil) != tc.wantSpend {
				t.Fatalf("spend visibility: %s", p.JSON())
			}
			if sp == nil {
				return
			}
			if (sp.UnavailableReason != "") != tc.unavailable || sp.LimitReached != tc.reached {
				t.Fatalf("spend availability/cap: %+v", sp)
			}
			if tc.unavailable {
				if sp.UsedAUD != 0 || sp.LimitAUD != 0 {
					t.Fatalf("invented money: %+v", sp)
				}
			} else if sp.UsedAUD != 72.84 || sp.LimitAUD != 100 {
				t.Fatalf("money conversion or override changed amount: %+v", sp)
			}
		})
	}
}

func TestSubscribedProjectionHistoryBounds(t *testing.T) {
	db, err := sqlpipe.OpenDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := controlplane.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(projectionSchema); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	monthStart := reset.Add(-30 * 24 * time.Hour)
	window := func(name string, duration time.Duration, resets any, remaining int) map[string]any {
		return map[string]any{"name": name, "limit_window": int64(duration), "resets_at": resets, "remaining_percent": remaining, "band": "ok"}
	}
	reading := func(windows ...map[string]any) string {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"windows": windows})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	insertHistory := func(provider string, at time.Time, raw string) {
		t.Helper()
		if err := db.Exec("INSERT INTO claudia_plan_usage_history(provider,observed_at,reading_json) VALUES (?,?,?)", provider, at.Format(time.RFC3339Nano), raw); err != nil {
			t.Fatal(err)
		}
	}
	monthly := reading(window("weekly", 30*24*time.Hour, reset, 40))
	nullReset := reading(window("session", 0, nil, 70))
	unknownDuration := reading(window("weekly", 0, reset, 60))
	for provider, raw := range map[string]string{"cursor": monthly, "grok": nullReset, "codex": unknownDuration} {
		stamp := now.Format(time.RFC3339Nano)
		if err := db.Exec("INSERT INTO claudia_plan_usage(provider,status,reason,attempted_at,success_at,next_attempt_at,reading_json) VALUES (?,'available','',?,?,?,?)", provider, stamp, stamp, stamp, raw); err != nil {
			t.Fatal(err)
		}
	}
	// A malformed, retained old payload is deliberate: expanding it would
	// fail the whole query, proving that the timestamp filter runs first.
	insertHistory("cursor", monthStart.Add(-time.Hour), "not JSON: excluded before expansion")
	insertHistory("cursor", monthStart, monthly)
	insertHistory("cursor", now.Add(-time.Hour).Add(125*time.Millisecond), monthly)
	insertHistory("cursor", now, monthly)
	insertHistory("cursor", monthStart.Add(time.Hour), reading(window("weekly", 30*24*time.Hour, reset.Add(-30*24*time.Hour), 80)))
	insertHistory("grok", now.AddDate(-2, 0, 0), nullReset)
	insertHistory("grok", now, nullReset)
	insertHistory("codex", now.AddDate(-2, 0, 0), unknownDuration)
	insertHistory("codex", now, unknownDuration)
	// Legacy/canonical timestamps in the same second are the same sample
	// even when their fractional precision differs. Dedup must use the index.
	if err := db.Exec("INSERT INTO jv_plan_history_legacy(provider,window,resets_key,fetched_at,remaining) VALUES ('cursor','monthly',?,?,99)", reset.Format(time.RFC3339), now.Add(-time.Hour).Add(875*time.Millisecond).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	qr, err := db.Query(ProjectionQuery)
	if err != nil {
		t.Fatal(err)
	}
	p := &Projection{}
	if err := p.accept(qr); err != nil {
		t.Fatal(err)
	}
	for _, be := range p.Snapshot().Backends {
		history := be.Windows[0].History
		want := 2
		if be.Provider == "cursor" {
			want = 3
		}
		if len(history) != want {
			t.Fatalf("%s history lost samples or included old period/duplicate: %s", be.Provider, p.JSON())
		}
		if be.Provider == "cursor" && !history[0].At.Equal(monthStart) {
			t.Fatalf("monthly start was truncated: %+v", history)
		}
		if be.Provider != "cursor" && !history[0].At.Equal(now.AddDate(-2, 0, 0)) {
			t.Fatalf("unknown reset/duration invented a bound: %+v", history)
		}
	}
	// Inspect the exact production history CTEs. sqldeep does not currently
	// support EXPLAIN around its final nested-object projection.
	ctes, _, ok := strings.Cut(ProjectionQuery, "\nSELECT {")
	if !ok {
		t.Fatal("missing final projection")
	}
	plan, err := db.Query("EXPLAIN QUERY PLAN " + ctes + "\nSELECT provider FROM ranked_history")
	if err != nil {
		t.Fatal(err)
	}
	indexedRanges := 0
	for _, row := range plan.Rows {
		detail := fmt.Sprint(row[len(row)-1])
		if strings.Contains(detail, "SEARCH h USING") && strings.Contains(detail, "provider=? AND observed_at>") {
			indexedRanges++
		}
	}
	if indexedRanges != 2 {
		t.Fatalf("expected indexed candidate and dedup ranges, got %d: %v", indexedRanges, plan.Rows)
	}
	count, err := db.Query("SELECT count(*) FROM claudia_plan_usage_history")
	if err != nil || count.Rows[0][0] != int64(9) {
		t.Fatalf("history must remain retained: %v %v", count.Rows, err)
	}
}
