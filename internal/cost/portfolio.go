// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Multi-provider LLM harness portfolio (🎯T325.2).
//
// Distinct from internal/provider (tool MCP providers: mnemo, bullseye).
// This package seeds a task-type → preferred harness provider/model table
// and a pure routing decision that prefers task fit then under-utilised
// capacity (session soft caps). Numbers are relative preference / load
// policy — not live vendor billing APIs.
//
// Caps are session-count only. T137 subscription accounting remains the
// sole honesty layer for USD: routing never invents list-price pause/kill
// from API-equivalent estimates.

// Task-type identifiers (seed from docs/design/life-and-work-org-map.md §5.2).
const (
	TaskCEO            = "ceo"             // fleet CEO / interruptible root
	TaskCodeImplement  = "code_implement"  // large multi-file code implement
	TaskMechanical     = "mechanical"      // hermetic oracle / greps / mechanical
	TaskDesignProse    = "design_prose"    // design map / doctrine prose
	TaskOpsClassify    = "ops_classify"    // sentinel / ops classify
	TaskJourneyGrok    = "journey_grok"    // journeys / live Grok path
	TaskIdeation       = "ideation"        // ideation / opportunity cost
)

// Harness provider ids (claudia backends). GPT class maps to "codex".
const (
	HarnessGrok   = "grok"
	HarnessClaude = "claude"
	HarnessCodex  = "codex" // GPT / OpenAI class in claudia
)

// TaskRoute is one row of the task-type preference table.
type TaskRoute struct {
	// Prefer is ordered harness provider ids (first = best fit).
	Prefer []string `json:"prefer"`
	// Secondary is fallback when Prefer is at soft cap or empty.
	Secondary []string `json:"secondary,omitempty"`
	// Model is an optional preferred model pin for this task type
	// (empty = leave to provider default / caller pin).
	Model string `json:"model,omitempty"`
}

// Portfolio is the multi-provider load/token routing seed.
type Portfolio struct {
	// Routes maps task type → preference row. Missing types fall back to
	// TaskCodeImplement, then DefaultProvider.
	Routes map[string]TaskRoute `json:"routes"`
	// SoftCaps is concurrent registered-agent soft limit per harness
	// provider. 0 or missing = unlimited for that provider.
	SoftCaps map[string]int `json:"soft_caps"`
	// DefaultProvider is the last-resort harness id.
	DefaultProvider string `json:"default_provider"`
}

// LoadCounts is live harness load (session / registered-agent counts).
// Keys are lowercase provider ids. Routing never consults USD burn rates.
type LoadCounts map[string]int

// RouteDecision is one pure portfolio pick (thin vertical for agent_start).
type RouteDecision struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	TaskType string `json:"task_type"`
	// Reason is human/oracle-readable: fit, under_utilised, soft_cap_spread, default.
	Reason string `json:"reason"`
	// CapHit is true when the first preferred provider was at soft cap.
	CapHit bool `json:"cap_hit,omitempty"`
}

// DefaultPortfolio returns the design-map seed (not owner-ratified prices).
// Soft caps are conservative session spreads so concurrent missions do not
// all pin one subscription until throttle without considering alternatives.
func DefaultPortfolio() *Portfolio {
	return &Portfolio{
		DefaultProvider: HarnessGrok,
		SoftCaps: map[string]int{
			HarnessGrok:   12,
			HarnessClaude: 8,
			HarnessCodex:  6,
		},
		Routes: map[string]TaskRoute{
			// Continuity + owner chat path (daily harness).
			TaskCEO: {
				Prefer:    []string{HarnessGrok},
				Secondary: []string{HarnessClaude, HarnessCodex},
			},
			// Strong code model when available; Grok remains secondary.
			TaskCodeImplement: {
				Prefer:    []string{HarnessClaude, HarnessCodex},
				Secondary: []string{HarnessGrok},
			},
			// Cheapest capable for mechanical work.
			TaskMechanical: {
				Prefer:    []string{HarnessGrok},
				Secondary: []string{HarnessClaude, HarnessCodex},
			},
			// Coherence over thrash for design/doctrine.
			TaskDesignProse: {
				Prefer:    []string{HarnessClaude, HarnessGrok},
				Secondary: []string{HarnessCodex},
			},
			// Bounded ops: small/fast when pure policy.
			TaskOpsClassify: {
				Prefer:    []string{HarnessGrok},
				Secondary: []string{HarnessClaude},
			},
			// Oracle honesty: provider under test (Grok path).
			TaskJourneyGrok: {
				Prefer: []string{HarnessGrok},
			},
			// Episodic ideation: mid + tools later.
			TaskIdeation: {
				Prefer:    []string{HarnessGrok, HarnessClaude},
				Secondary: []string{HarnessCodex},
			},
		},
	}
}

// NormalizeTaskType maps free-form / purpose aliases to a seed task type.
// Empty or unknown → TaskCodeImplement (default Build work).
func NormalizeTaskType(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case TaskCEO, "overseer", "root", "fleet_ceo":
		return TaskCEO
	case TaskCodeImplement, "", "work", "implement", "build", "code",
		"code-implement", "multi_file", "refactor":
		return TaskCodeImplement
	case TaskMechanical, "oracle", "grep", "hermetic", "mechanical_oracle":
		return TaskMechanical
	case TaskDesignProse, "design", "doctrine", "prose", "design_map":
		return TaskDesignProse
	case TaskOpsClassify, "ops", "sentinel", "staff", "classify":
		return TaskOpsClassify
	case TaskJourneyGrok, "journey", "live_grok", "journeys":
		return TaskJourneyGrok
	case TaskIdeation, "aside", "idea", "ideate":
		return TaskIdeation
	default:
		// Unknown free-form still routes as code implement (Build default).
		return TaskCodeImplement
	}
}

// TaskTypeFromPurpose maps claudia purpose to a seed task type when the
// caller did not pass an explicit task_type.
func TaskTypeFromPurpose(purpose string) string {
	switch strings.ToLower(strings.TrimSpace(purpose)) {
	case "overseer":
		return TaskCEO
	case "aside":
		return TaskIdeation
	default:
		return TaskCodeImplement
	}
}

// Count is the load for provider (0 if unknown).
func (l LoadCounts) Count(provider string) int {
	if l == nil {
		return 0
	}
	return l[strings.ToLower(strings.TrimSpace(provider))]
}

// atCap reports whether provider is at or over its soft cap.
func (p *Portfolio) atCap(provider string, load LoadCounts) bool {
	if p == nil || p.SoftCaps == nil {
		return false
	}
	capN, ok := p.SoftCaps[strings.ToLower(strings.TrimSpace(provider))]
	if !ok || capN <= 0 {
		return false
	}
	return load.Count(provider) >= capN
}

// Route picks a harness provider for taskType given live load.
//
// Policy:
//  1. Walk Prefer in order; first under soft cap wins (reason=fit or under_utilised).
//  2. If all Prefer at cap, walk Secondary the same way (reason=soft_cap_spread).
//  3. Else pick least-loaded among Prefer∪Secondary (spread).
//  4. Else DefaultProvider.
//
// Explicit caller override is the caller's job (agent_start provider=);
// this pure path never reassigns mid-flight agents.
func (p *Portfolio) Route(taskType string, load LoadCounts) RouteDecision {
	if p == nil {
		p = DefaultPortfolio()
	}
	tt := NormalizeTaskType(taskType)
	route, ok := p.Routes[tt]
	if !ok {
		route = p.Routes[TaskCodeImplement]
		tt = TaskCodeImplement
	}
	defProv := strings.TrimSpace(p.DefaultProvider)
	if defProv == "" {
		defProv = HarnessGrok
	}

	// 1) Prefer under cap.
	for i, prov := range route.Prefer {
		prov = strings.ToLower(strings.TrimSpace(prov))
		if prov == "" {
			continue
		}
		if !p.atCap(prov, load) {
			reason := "fit"
			if i > 0 || load.Count(prov) > 0 {
				// Secondary prefer rank or already has load → still fit if under cap;
				// use under_utilised when a higher-prefer was at cap.
				if i > 0 && p.atCap(route.Prefer[0], load) {
					reason = "under_utilised"
				} else if i > 0 {
					reason = "fit"
				}
			}
			// If first prefer is at cap we wouldn't be here on i=0.
			capHit := false
			if i > 0 {
				// Cap hit only if an earlier prefer was capped.
				for _, earlier := range route.Prefer[:i] {
					if p.atCap(earlier, load) {
						capHit = true
						reason = "under_utilised"
						break
					}
				}
			}
			return RouteDecision{
				Provider: prov,
				Model:    strings.TrimSpace(route.Model),
				TaskType: tt,
				Reason:   reason,
				CapHit:   capHit,
			}
		}
	}

	// 2) Secondary under cap.
	for _, prov := range route.Secondary {
		prov = strings.ToLower(strings.TrimSpace(prov))
		if prov == "" {
			continue
		}
		if !p.atCap(prov, load) {
			return RouteDecision{
				Provider: prov,
				Model:    strings.TrimSpace(route.Model),
				TaskType: tt,
				Reason:   "soft_cap_spread",
				CapHit:   true,
			}
		}
	}

	// 3) Least-loaded among all candidates (even if over soft cap).
	candidates := append([]string{}, route.Prefer...)
	candidates = append(candidates, route.Secondary...)
	best := ""
	bestN := int(^uint(0) >> 1)
	for _, prov := range candidates {
		prov = strings.ToLower(strings.TrimSpace(prov))
		if prov == "" {
			continue
		}
		n := load.Count(prov)
		if best == "" || n < bestN {
			best = prov
			bestN = n
		}
	}
	if best != "" {
		return RouteDecision{
			Provider: best,
			Model:    strings.TrimSpace(route.Model),
			TaskType: tt,
			Reason:   "soft_cap_spread",
			CapHit:   true,
		}
	}

	// 4) Default.
	return RouteDecision{
		Provider: defProv,
		Model:    strings.TrimSpace(route.Model),
		TaskType: tt,
		Reason:   "default",
	}
}

// MergeSoftCaps returns a copy of p with soft caps overlaid from overrides
// (e.g. budget.json provider_soft_caps). Zero/negative override values
// remove the cap (unlimited). Empty overrides leave p unchanged (copy).
func (p *Portfolio) MergeSoftCaps(overrides map[string]int) *Portfolio {
	base := DefaultPortfolio()
	if p != nil {
		base = p.clone()
	}
	if len(overrides) == 0 {
		return base
	}
	if base.SoftCaps == nil {
		base.SoftCaps = map[string]int{}
	}
	for k, v := range overrides {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		if v <= 0 {
			delete(base.SoftCaps, key)
			continue
		}
		base.SoftCaps[key] = v
	}
	return base
}

func (p *Portfolio) clone() *Portfolio {
	if p == nil {
		return DefaultPortfolio()
	}
	out := &Portfolio{
		DefaultProvider: p.DefaultProvider,
		Routes:          make(map[string]TaskRoute, len(p.Routes)),
		SoftCaps:        make(map[string]int, len(p.SoftCaps)),
	}
	for k, v := range p.Routes {
		out.Routes[k] = TaskRoute{
			Prefer:    append([]string(nil), v.Prefer...),
			Secondary: append([]string(nil), v.Secondary...),
			Model:     v.Model,
		}
	}
	for k, v := range p.SoftCaps {
		out.SoftCaps[k] = v
	}
	return out
}

// LoadPortfolioOverride reads an optional JSON portfolio override.
// loaded is true only when the file existed and parsed. Missing file
// returns the compiled seed with loaded=false (not an error). Malformed
// is an error. 🎯T476 needs loaded so a leftover file can be named as
// a losing knob instead of being indistinguishable from the seed.
func LoadPortfolioOverride(path string) (p *Portfolio, loaded bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultPortfolio(), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	p = DefaultPortfolio()
	if err := json.Unmarshal(data, p); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	if p.DefaultProvider == "" {
		p.DefaultProvider = HarnessGrok
	}
	if p.Routes == nil {
		p.Routes = DefaultPortfolio().Routes
	}
	return p, true, nil
}

// LoadPortfolioFile reads an optional JSON portfolio override.
// Missing file → DefaultPortfolio (not an error). Malformed → error.
func LoadPortfolioFile(path string) (*Portfolio, error) {
	p, _, err := LoadPortfolioOverride(path)
	return p, err
}

// ProviderSoftCapsField is the budget.json key for soft-cap overlays
// (documented for owners; loaded via BudgetConfig).
const ProviderSoftCapsJSONKey = "provider_soft_caps"
