// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// codexLine is the subset of a Codex rollout JSONL line used for usage.
// token_count events carry cumulative total_token_usage and per-turn
// last_token_usage plus optional rate_limits (plan window).
type codexLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage *codexTokenUsage `json:"total_token_usage"`
			LastTokenUsage  *codexTokenUsage `json:"last_token_usage"`
		} `json:"info"`
		RateLimits *codexRateLimits `json:"rate_limits"`
		SessionID  string           `json:"session_id"`
	} `json:"payload"`
}

type codexTokenUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
}

type codexRateLimits struct {
	PlanType string `json:"plan_type"`
	Primary  *struct {
		UsedPercent   float64 `json:"used_percent"`
		WindowMinutes int     `json:"window_minutes"`
		ResetsAt      int64   `json:"resets_at"`
	} `json:"primary"`
	Credits *struct {
		Balance string `json:"balance"`
	} `json:"credits"`
}

// collectCodex scrapes Codex rollout-*.jsonl under ~/.codex (or Roots[codex]).
// Per session file we sum last_token_usage deltas and keep the latest
// rate_limits snapshot (same signal as Codex /status).
func collectCodex(args *CollectArgs) (Report, error) {
	root, err := args.root(HarnessCodex, ".codex")
	if err != nil {
		return Report{}, err
	}
	root = resolveCodexHome(root)

	r := Report{
		Harness:     HarnessCodex,
		Source:      "local-jsonl",
		GeneratedAt: args.now(),
		Root:        root,
	}
	if args.TryLiveAPI {
		if note := tryOpenAIUsageAPI(); note != "" {
			r.Notes = append(r.Notes, note)
		}
	}

	sessions := map[string]struct{}{}
	var since time.Time
	if args.Since != nil {
		since = *args.Since
	}
	var latestRL *RateLimitSnapshot
	var latestRLAt time.Time

	_, walkErr := walkMatching(root, args.MaxFiles, func(path string, _ fs.DirEntry) bool {
		return isCodexRolloutJSONL(path)
	}, func(path string) error {
		sid := codexSessionIDFromPath(path)
		return forEachJSONLLineErr(path, func(line []byte) error {
			var l codexLine
			if err := json.Unmarshal(line, &l); err != nil {
				return nil
			}
			// session_meta carries session_id
			if l.Type == "session_meta" && l.Payload.SessionID != "" {
				sid = l.Payload.SessionID
				return nil
			}
			if l.Type != "event_msg" || l.Payload.Type != "token_count" || l.Payload.Info == nil {
				return nil
			}
			ts := parseRFC3339(l.Timestamp, args.now())
			if !since.IsZero() && ts.Before(since) {
				return nil
			}
			u := l.Payload.Info.LastTokenUsage
			if u == nil {
				// Fall back to total if last is missing (rare).
				u = l.Payload.Info.TotalTokenUsage
			}
			if u == nil {
				return nil
			}
			r.Events++
			r.Tokens.Input += u.InputTokens
			r.Tokens.Output += u.OutputTokens
			r.Tokens.CacheRead += u.CachedInputTokens
			r.Tokens.CacheCreate += u.CacheWriteInputTokens
			r.Tokens.Reasoning += u.ReasoningOutputTokens
			sessions[sid] = struct{}{}

			if rl := l.Payload.RateLimits; rl != nil {
				snap := rateLimitFromCodex(rl)
				if latestRL == nil || !ts.Before(latestRLAt) {
					latestRL = snap
					latestRLAt = ts
				}
			}
			return nil
		})
	})
	if walkErr != nil {
		return Report{}, walkErr
	}
	r.Sessions = len(sessions)
	r.RateLimits = latestRL
	if r.Events == 0 {
		r.Notes = append(r.Notes, "no Codex token_count events under "+root)
	}
	return r, nil
}

func codexSessionIDFromPath(path string) string {
	// rollout-2026-07-23T13-56-26-<uuid>.jsonl or rollout-sess-c.jsonl
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	base = strings.TrimPrefix(base, "rollout-")
	// Prefer trailing UUID-like segment when present.
	if i := strings.LastIndex(base, "-"); i >= 0 && len(base)-i-1 >= 8 {
		// multi-segment timestamps — take last 5 hyphen parts if uuid shape
		parts := strings.Split(base, "-")
		if len(parts) >= 5 {
			// uuid is 5 segments at end: 8-4-4-4-12
			cand := strings.Join(parts[len(parts)-5:], "-")
			if len(cand) >= 32 {
				return cand
			}
		}
	}
	return base
}

func rateLimitFromCodex(rl *codexRateLimits) *RateLimitSnapshot {
	snap := &RateLimitSnapshot{PlanType: rl.PlanType}
	if rl.Primary != nil {
		p := rl.Primary.UsedPercent
		snap.PrimaryUsedPercent = &p
		snap.PrimaryWindowMin = rl.Primary.WindowMinutes
		rs := rl.Primary.ResetsAt
		snap.PrimaryResetsAt = &rs
	}
	if rl.Credits != nil {
		snap.CreditsBalance = rl.Credits.Balance
	}
	return snap
}

func parseRFC3339(s string, fallback time.Time) time.Time {
	if s == "" {
		return fallback
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return fallback
}
