// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package harnessusage obtains on-demand usage reports for coding
// harnesses used by the Jevons fleet (🎯T163).
//
// Strategy per harness:
//  1. Prefer official/API usage surfaces when credentials are present.
//  2. Otherwise scrape local harness logs / state (JSONL, SQLite).
//  3. Fixture-backed paths remain available when live auth or rate
//     limits block remote hits — tests and offline repros use CollectArgs
//     roots / CursorDashboardJSON exclusively.
//
// This package is observational (report-on-demand). Live spend
// enforcement still lives in package cost (🎯T36).
package harnessusage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Harness identifies a supported coding agent backend.
type Harness string

const (
	HarnessClaude Harness = "claude"
	HarnessGrok   Harness = "grok"
	HarnessCodex  Harness = "codex"
	HarnessCursor Harness = "cursor"
)

// AllHarnesses is the fleet minimum set for 🎯T163.
var AllHarnesses = []Harness{
	HarnessClaude,
	HarnessGrok,
	HarnessCodex,
	HarnessCursor,
}

// ParseHarness accepts common aliases (claude-code, acp, …).
func ParseHarness(s string) (Harness, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "claude", "claude-code", "anthropic":
		return HarnessClaude, nil
	case "grok", "grok-build", "acp", "xai":
		return HarnessGrok, nil
	case "codex", "openai-codex", "openai":
		return HarnessCodex, nil
	case "cursor":
		return HarnessCursor, nil
	case "all", "*":
		return "", fmt.Errorf("use CollectAll, not ParseHarness(%q)", s)
	default:
		return "", fmt.Errorf("unknown harness %q (want claude|grok|codex|cursor)", s)
	}
}

// TokenTotals aggregates token classes across billable events.
type TokenTotals struct {
	Input       int64 `json:"input_tokens"`
	Output      int64 `json:"output_tokens"`
	CacheCreate int64 `json:"cache_creation_input_tokens"`
	CacheRead   int64 `json:"cache_read_input_tokens"`
	// Reasoning is Codex-only (reasoning_output_tokens); others leave 0.
	Reasoning int64 `json:"reasoning_output_tokens,omitempty"`
}

// Total is the sum of all token classes.
func (t TokenTotals) Total() int64 {
	return t.Input + t.Output + t.CacheCreate + t.CacheRead + t.Reasoning
}

// RateLimitSnapshot is the latest plan-window usage signal (Codex /status).
type RateLimitSnapshot struct {
	PlanType           string   `json:"plan_type,omitempty"`
	PrimaryUsedPercent *float64 `json:"primary_used_percent,omitempty"`
	PrimaryWindowMin   int      `json:"primary_window_minutes,omitempty"`
	PrimaryResetsAt    *int64   `json:"primary_resets_at,omitempty"`
	CreditsBalance     string   `json:"credits_balance,omitempty"`
}

// ModelBreakdown is optional per-model rollup (Cursor dashboard fixtures).
type ModelBreakdown struct {
	Model        string `json:"model"`
	Requests     int64  `json:"requests,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	Events       int64  `json:"events,omitempty"`
}

// Report is one harness's on-demand usage summary.
type Report struct {
	Harness     Harness            `json:"harness"`
	Source      string             `json:"source"`
	GeneratedAt time.Time          `json:"generated_at"`
	Root        string             `json:"root,omitempty"`
	Sessions    int                `json:"sessions"`
	Events      int                `json:"events"`
	Tokens      TokenTotals        `json:"tokens"`
	CostUSD     *float64           `json:"cost_usd,omitempty"`
	RateLimits  *RateLimitSnapshot `json:"rate_limits,omitempty"`
	Models      []ModelBreakdown   `json:"models,omitempty"`
	Notes       []string           `json:"notes,omitempty"`
}

// CollectArgs configures a report run. Zero values use defaults under $HOME.
// For hermetic tests, set Roots / CursorDashboardJSON / CursorTrackingDB.
type CollectArgs struct {
	// Roots maps harness → data root override (fixture tree).
	// Claude: projects parent (~/.claude); Grok: sessions parent (~/.grok);
	// Codex: codex home (~/.codex); Cursor: ignored when dashboard/db set.
	Roots map[Harness]string

	// Home overrides os.UserHomeDir for default path resolution.
	Home string

	// CursorDashboardJSON is a fixture or export of the Cursor usage surface
	// (settings → Usage / CSV-shaped JSON). Preferred over local SQLite when set.
	CursorDashboardJSON string

	// CursorTrackingDB is path to Cursor's ai-code-tracking.db (activity,
	// not billing). Used when no dashboard JSON is available.
	CursorTrackingDB string

	// Since, when non-nil, drops events strictly before this time.
	Since *time.Time

	// MaxFiles caps how many session files are walked (0 = unlimited).
	// Useful for smoke runs on huge ~/.claude trees.
	MaxFiles int

	// Now stamps GeneratedAt (tests inject a fixed clock).
	Now time.Time

	// TryLiveAPI attempts official HTTP usage endpoints when credentials
	// are present. Failures degrade to local scrape with a note — never
	// hard-fail the report for auth/rate limits (🎯T163 residual).
	TryLiveAPI bool
}

func (a *CollectArgs) now() time.Time {
	if a != nil && !a.Now.IsZero() {
		return a.Now
	}
	return time.Now().UTC()
}

func (a *CollectArgs) home() (string, error) {
	if a != nil && a.Home != "" {
		return a.Home, nil
	}
	return os.UserHomeDir()
}

func (a *CollectArgs) root(h Harness, defaultRel string) (string, error) {
	if a != nil && a.Roots != nil {
		if r, ok := a.Roots[h]; ok && r != "" {
			return r, nil
		}
	}
	home, err := a.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultRel), nil
}

// Collect returns a usage report for one harness.
func Collect(h Harness, args *CollectArgs) (Report, error) {
	if args == nil {
		args = &CollectArgs{}
	}
	switch h {
	case HarnessClaude:
		return collectClaude(args)
	case HarnessGrok:
		return collectGrok(args)
	case HarnessCodex:
		return collectCodex(args)
	case HarnessCursor:
		return collectCursor(args)
	default:
		return Report{}, fmt.Errorf("unknown harness %q", h)
	}
}

// CollectAll reports every harness in AllHarnesses. Partial failures
// surface as Reports with Source=error and a note (other harnesses still run).
func CollectAll(args *CollectArgs) []Report {
	out := make([]Report, 0, len(AllHarnesses))
	for _, h := range AllHarnesses {
		r, err := Collect(h, args)
		if err != nil {
			out = append(out, Report{
				Harness:     h,
				Source:      "error",
				GeneratedAt: args.now(),
				Notes:       []string{err.Error()},
			})
			continue
		}
		out = append(out, r)
	}
	return out
}

func addCost(dst **float64, add float64) {
	if *dst == nil {
		v := add
		*dst = &v
		return
	}
	**dst += add
}

func sortModelKeys(m map[string]*ModelBreakdown) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func modelsFromMap(m map[string]*ModelBreakdown) []ModelBreakdown {
	keys := sortModelKeys(m)
	out := make([]ModelBreakdown, 0, len(keys))
	for _, k := range keys {
		out = append(out, *m[k])
	}
	return out
}
