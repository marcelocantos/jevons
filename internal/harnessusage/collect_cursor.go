// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo
)

// cursorDashboard is the fixture / export shape for Cursor settings → Usage.
type cursorDashboard struct {
	Source            string  `json:"source"`
	BillingCycleStart string  `json:"billing_cycle_start"`
	BillingCycleEnd   string  `json:"billing_cycle_end"`
	Plan              string  `json:"plan"`
	IncludedRequests  int64   `json:"included_requests"`
	UsedRequests      int64   `json:"used_requests"`
	OnDemandSpendUSD  float64 `json:"on_demand_spend_usd"`
	Models            []struct {
		Model        string `json:"model"`
		Requests     int64  `json:"requests"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
	} `json:"models"`
}

// collectCursor prefers CursorDashboardJSON (billing surface export), else
// ai-code-tracking.db (activity hashes — not full billing tokens).
func collectCursor(args *CollectArgs) (Report, error) {
	r := Report{
		Harness:     HarnessCursor,
		GeneratedAt: args.now(),
	}
	if args.TryLiveAPI {
		if note := tryCursorUsageAPI(); note != "" {
			r.Notes = append(r.Notes, note)
		}
	}

	dashPath := ""
	if args != nil {
		dashPath = args.CursorDashboardJSON
	}
	if dashPath == "" {
		// Optional default export path under Home (documented, rarely present).
		if home, err := args.home(); err == nil {
			cand := filepath.Join(home, ".cursor", "dashboard-usage.json")
			if _, err := os.Stat(cand); err == nil {
				dashPath = cand
			}
		}
	}
	if dashPath != "" {
		rep, err := collectCursorDashboard(args, dashPath)
		if err != nil {
			return Report{}, err
		}
		rep.Notes = append(r.Notes, rep.Notes...)
		return rep, nil
	}

	dbPath := ""
	if args != nil {
		dbPath = args.CursorTrackingDB
	}
	if dbPath == "" {
		if home, err := args.home(); err == nil {
			cand := filepath.Join(home, ".cursor", "ai-tracking", "ai-code-tracking.db")
			if _, err := os.Stat(cand); err == nil {
				dbPath = cand
			}
		}
	}
	if dbPath == "" {
		r.Source = "unavailable"
		r.Notes = append(r.Notes,
			"no Cursor dashboard JSON or ai-code-tracking.db; export Usage from Cursor settings or set CursorDashboardJSON / CursorTrackingDB")
		return r, nil
	}
	rep, err := collectCursorTrackingDB(args, dbPath)
	if err != nil {
		return Report{}, err
	}
	rep.Notes = append(r.Notes, rep.Notes...)
	return rep, nil
}

func collectCursorDashboard(args *CollectArgs, path string) (Report, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var d cursorDashboard
	if err := json.Unmarshal(raw, &d); err != nil {
		return Report{}, fmt.Errorf("cursor dashboard JSON: %w", err)
	}
	r := Report{
		Harness:     HarnessCursor,
		Source:      "cursor-dashboard",
		GeneratedAt: args.now(),
		Root:        path,
	}
	if d.Source != "" {
		r.Source = d.Source
	}
	if d.OnDemandSpendUSD > 0 {
		v := d.OnDemandSpendUSD
		r.CostUSD = &v
	}
	// Requests are the Cursor plan unit of account; map into Events.
	r.Events = int(d.UsedRequests)
	models := map[string]*ModelBreakdown{}
	var in, out int64
	for _, m := range d.Models {
		name := m.Model
		if name == "" {
			name = "unknown"
		}
		mb := &ModelBreakdown{
			Model:        name,
			Requests:     m.Requests,
			InputTokens:  m.InputTokens,
			OutputTokens: m.OutputTokens,
			Events:       m.Requests,
		}
		models[name] = mb
		in += m.InputTokens
		out += m.OutputTokens
	}
	r.Tokens.Input = in
	r.Tokens.Output = out
	r.Models = modelsFromMap(models)
	r.Sessions = len(models) // no conversation list in dashboard fixture
	if d.Plan != "" {
		r.RateLimits = &RateLimitSnapshot{PlanType: d.Plan}
		if d.IncludedRequests > 0 {
			pct := 100 * float64(d.UsedRequests) / float64(d.IncludedRequests)
			r.RateLimits.PrimaryUsedPercent = &pct
		}
	}
	r.Notes = append(r.Notes,
		fmt.Sprintf("included_requests=%d used_requests=%d cycle=%s..%s",
			d.IncludedRequests, d.UsedRequests, d.BillingCycleStart, d.BillingCycleEnd))
	return r, nil
}

func collectCursorTrackingDB(args *CollectArgs, path string) (Report, error) {
	// Open read-only; activity DB is not billing truth.
	dsn := path + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return Report{}, err
	}
	defer db.Close()

	r := Report{
		Harness:     HarnessCursor,
		Source:      "cursor-tracking-db",
		GeneratedAt: args.now(),
		Root:        path,
	}
	r.Notes = append(r.Notes,
		"ai-code-tracking.db records code hashes / activity, not full token billing; prefer dashboard export for spend")

	// Conversations as sessions.
	var sessions int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT conversationId) FROM ai_code_hashes WHERE conversationId IS NOT NULL AND conversationId != ''`).Scan(&sessions); err != nil {
		// Table missing or empty schema.
		r.Notes = append(r.Notes, "tracking db query failed: "+err.Error())
		return r, nil
	}
	r.Sessions = sessions

	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_code_hashes`).Scan(&events); err != nil {
		r.Notes = append(r.Notes, "tracking db count failed: "+err.Error())
		return r, nil
	}
	r.Events = events

	rows, err := db.Query(`
		SELECT COALESCE(NULLIF(model, ''), 'default') AS model, COUNT(*) AS n
		FROM ai_code_hashes
		GROUP BY 1
		ORDER BY 1`)
	if err != nil {
		r.Notes = append(r.Notes, "tracking db models failed: "+err.Error())
		return r, nil
	}
	defer rows.Close()
	var models []ModelBreakdown
	for rows.Next() {
		var m ModelBreakdown
		if err := rows.Scan(&m.Model, &m.Events); err != nil {
			return Report{}, err
		}
		m.Requests = m.Events
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return Report{}, err
	}
	r.Models = models
	return r, nil
}
