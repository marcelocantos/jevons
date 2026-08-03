// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"io/fs"
	"time"

	"github.com/marcelocantos/jevons/internal/cost"
)

// collectGrok scrapes Grok Build updates.jsonl under ~/.grok/sessions
// (or Roots[grok]). Prefers costUsdTicks from turn_completed events.
func collectGrok(args *CollectArgs) (Report, error) {
	root, err := args.root(HarnessGrok, ".grok")
	if err != nil {
		return Report{}, err
	}
	sessionsRoot := resolveGrokSessions(root)

	r := Report{
		Harness:     HarnessGrok,
		Source:      "local-jsonl",
		GeneratedAt: args.now(),
		Root:        sessionsRoot,
	}
	if args.TryLiveAPI {
		if note := tryXAIUsageAPI(); note != "" {
			r.Notes = append(r.Notes, note)
		}
	}

	models := map[string]*ModelBreakdown{}
	seenReq := map[string]struct{}{}
	sessions := map[string]struct{}{}
	var since time.Time
	if args.Since != nil {
		since = *args.Since
	}

	_, walkErr := walkMatching(sessionsRoot, args.MaxFiles, func(path string, _ fs.DirEntry) bool {
		return isGrokUpdatesJSONL(path)
	}, func(path string) error {
		sid := sessionIDFromGrokPath(path)
		return forEachJSONLLine(path, func(line []byte) error {
			ev := cost.ParseLine(line, sid, args.now())
			if ev == nil {
				return nil
			}
			if !since.IsZero() && ev.Timestamp.Before(since) {
				return nil
			}
			if ev.RequestID != "" {
				if _, ok := seenReq[ev.RequestID]; ok {
					return nil
				}
				seenReq[ev.RequestID] = struct{}{}
			}
			if ev.SessionID != "" {
				sessions[ev.SessionID] = struct{}{}
			} else {
				sessions[sid] = struct{}{}
			}
			r.Events++
			r.Tokens.Input += ev.Usage.Input
			r.Tokens.Output += ev.Usage.Output
			r.Tokens.CacheCreate += ev.Usage.CacheCreate
			r.Tokens.CacheRead += ev.Usage.CacheRead
			if ev.CostUSD > 0 {
				addCost(&r.CostUSD, ev.CostUSD)
			}
			model := ev.Model
			if model == "" {
				model = "grok"
			}
			mb := models[model]
			if mb == nil {
				mb = &ModelBreakdown{Model: model}
				models[model] = mb
			}
			mb.Events++
			mb.InputTokens += ev.Usage.Input
			mb.OutputTokens += ev.Usage.Output
			return nil
		})
	})
	if walkErr != nil {
		return Report{}, walkErr
	}
	r.Sessions = len(sessions)
	r.Models = modelsFromMap(models)
	if r.Events == 0 {
		r.Notes = append(r.Notes, "no Grok turn_completed usage under "+sessionsRoot)
	}
	return r, nil
}
