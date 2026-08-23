// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"strings"
)

// Owner-visible knobs that can select a mint provider (🎯T476).
const (
	KnobExplicit      = "explicit"
	KnobResume        = "resume"
	KnobConfig        = "config"
	KnobPortfolioFile = "portfolio_file"
	KnobCompiledSeed  = "compiled_seed"
	KnobPlanDest      = "plan_dest"
)

// MintProviderArgs is the input to PickMintProvider.
type MintProviderArgs struct {
	// ProviderArg is the per-start override (jevons_agent_start provider=).
	ProviderArg string
	// Existed is true when the registry already has this name (resume).
	Existed bool
	// StoredProvider is the registry row's provider (resume).
	StoredProvider string
	// ConfigProvider is the already-resolved owner-visible default
	// (config.yaml provider → JEVONS_PROVIDER → grok).
	ConfigProvider string
	// Portfolio is what T325.2 Route would have picked for this mint.
	Portfolio RouteDecision
	// PortfolioFromFile is true when state_dir/llm-portfolio.json loaded.
	PortfolioFromFile bool
	// PlanFeedOK is true when the plan-usage feed produced candidates —
	// omit-provider mint is then usage-first (🎯T495).
	PlanFeedOK bool
	// PlanDest is the usage-first pick among green providers (🎯T495,
	// PickMintDest): highest remaining %, config only breaking green ties.
	PlanDest string
	// PlanDestOK is false when every published dest fails the mint
	// threshold — omit-provider mint must refuse, not land on a hot dest.
	PlanDestOK bool
}

// MintProviderPick is the owner-visible mint provider decision (🎯T476).
type MintProviderPick struct {
	Provider       string
	Knob           string // winning knob
	LosingKnob     string
	LosingProvider string
	TaskType       string
}

// PickMintProvider chooses the harness for a mint/resume.
//
// Precedence (🎯T476, usage-first per 🎯T495):
//  1. non-empty ProviderArg → explicit
//  2. resume with a stored provider → keep (never mid-flight reassign)
//  3. mint that omits provider → the plan feed's usage-first green pick
//     (PickMintDest: highest remaining %, config only breaking green
//     ties); when no green exists the mint refuses rather than landing
//     on an ineligible default
//  4. config.yaml / daemon default only when the plan feed is silent
//
// A leftover llm-portfolio.json or the compiled DefaultPortfolio seed
// (which still prefers Claude for code_implement / design_prose) must
// not silently win. When they would have picked a different provider,
// the loser is named on the pick so the start result can cite both knobs.
func PickMintProvider(a MintProviderArgs) MintProviderPick {
	if p := strings.ToLower(strings.TrimSpace(a.ProviderArg)); p != "" {
		pick := MintProviderPick{Provider: p, Knob: KnobExplicit, TaskType: a.Portfolio.TaskType}
		pick.noteLoser(a)
		return pick
	}
	if a.Existed {
		if p := strings.ToLower(strings.TrimSpace(a.StoredProvider)); p != "" {
			return MintProviderPick{Provider: p, Knob: KnobResume, TaskType: a.Portfolio.TaskType}
		}
	}
	cfg := strings.ToLower(strings.TrimSpace(a.ConfigProvider))
	if cfg == "" {
		cfg = HarnessGrok
	}
	if a.PlanFeedOK {
		if !a.PlanDestOK {
			return MintProviderPick{
				Provider:       "",
				Knob:           KnobPlanDest,
				LosingKnob:     KnobConfig,
				LosingProvider: cfg,
				TaskType:       a.Portfolio.TaskType,
			}
		}
		if dest := strings.ToLower(strings.TrimSpace(a.PlanDest)); dest != "" && dest != cfg {
			return MintProviderPick{
				Provider:       dest,
				Knob:           KnobPlanDest,
				LosingKnob:     KnobConfig,
				LosingProvider: cfg,
				TaskType:       a.Portfolio.TaskType,
			}
		}
	}
	pick := MintProviderPick{Provider: cfg, Knob: KnobConfig, TaskType: a.Portfolio.TaskType}
	pick.noteLoser(a)
	return pick
}

func (p *MintProviderPick) noteLoser(a MintProviderArgs) {
	want := strings.ToLower(strings.TrimSpace(a.Portfolio.Provider))
	if want == "" || want == p.Provider {
		return
	}
	p.LosingProvider = want
	if a.PortfolioFromFile {
		p.LosingKnob = KnobPortfolioFile
	} else {
		p.LosingKnob = KnobCompiledSeed
	}
	if p.TaskType == "" {
		p.TaskType = a.Portfolio.TaskType
	}
}

// Cite is the start-result fragment that names the winning knob,
// the portfolio task class (🎯T475), and — when they disagree — the
// leftover file or compiled seed that lost.
func (p MintProviderPick) Cite() string {
	s := "provider_knob: " + p.Knob
	if tt := strings.TrimSpace(p.TaskType); tt != "" {
		s += ", task_type: " + tt
	}
	if p.LosingKnob == "" || p.LosingProvider == "" {
		return s
	}
	s += fmt.Sprintf(" (%s would have picked %s", p.LosingKnob, p.LosingProvider)
	if tt := strings.TrimSpace(p.TaskType); tt != "" {
		s += " via " + tt
	}
	s += ")"
	return s
}

// WorkMintPortfolioProvider is what T325.2 would pick for a default
// work mint (code_implement, empty load) — the silent override T476 stops.
func WorkMintPortfolioProvider(p *Portfolio) string {
	if p == nil {
		p = DefaultPortfolio()
	}
	return p.Route(TaskCodeImplement, nil).Provider
}

// DescribeConfigPortfolioDisagreement is the boot-time loud note when
// llm-portfolio.json would pick a different work-mint provider than
// config.yaml. Empty when the file is absent or they agree. The
// portfolio file is not the omit-provider routing seed (🎯T476).
func DescribeConfigPortfolioDisagreement(configProvider string, p *Portfolio, fromFile bool) string {
	if !fromFile || p == nil {
		return ""
	}
	cfg := strings.ToLower(strings.TrimSpace(configProvider))
	if cfg == "" {
		cfg = HarnessGrok
	}
	dec := p.Route(TaskCodeImplement, nil)
	work := strings.ToLower(strings.TrimSpace(dec.Provider))
	def := strings.ToLower(strings.TrimSpace(p.DefaultProvider))
	if work == cfg && (def == "" || def == cfg) {
		return ""
	}
	loser := work
	if loser == "" || loser == cfg {
		loser = def
	}
	tt := dec.TaskType
	if tt == "" {
		tt = TaskCodeImplement
	}
	return fmt.Sprintf("provider_knob: %s (%s would have picked %s via %s)",
		KnobConfig, KnobPortfolioFile, loser, tt)
}
