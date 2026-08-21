// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Extra mint-model knobs (🎯T325.2.1). Provider knobs live in mint_provider.go.
const (
	KnobFastCheap   = "fast_cheap"
	KnobEscalated   = "escalated"
	KnobIneligible  = "ineligible"
	KnobModelResume = "resume"
)

// MintModelArgs is the input to PickMintModel.
type MintModelArgs struct {
	// ModelArg is the per-start override (jevons_agent_start model=).
	ModelArg string
	// Existed is true when the registry already has this name (resume).
	Existed bool
	// StoredModel is the registry row's model (resume).
	StoredModel string
	// Provider is the already-picked harness (T476 / T495).
	Provider string
	// TaskType is the (possibly raw) portfolio task class.
	TaskType string
	// PromptTokens is the estimated size of the turn/prompt about to
	// land. 0 = unknown/small — not a hard exceed.
	PromptTokens int
	// CodexEligible is false when the Codex weekly is red/exhausted
	// (🎯T390.1.5). Unknown (no plan feed) is true.
	CodexEligible bool
	// Portfolio supplies per-route Models overlays; nil uses compiled peers.
	Portfolio *Portfolio
}

// MintModelPick is the omit-model mint decision (🎯T325.2.1).
type MintModelPick struct {
	Model  string
	Knob   string
	Reason string
}

// PickMintModel chooses the model pin for a mint/resume.
//
// Precedence:
//  1. non-empty ModelArg → explicit (T476)
//  2. resume with a stored model → keep (never mid-flight re-pin)
//  3. fast-cheap task → Spark / Grok fast peer for the picked provider,
//     unless the prompt exceeds that model's context window (visible
//     escalate to empty = provider frontier default) or Codex weekly is
//     ineligible (do not pin Spark onto a red Codex dest)
//  4. otherwise empty (caller applies the provider default)
func PickMintModel(a MintModelArgs) MintModelPick {
	if m := strings.TrimSpace(a.ModelArg); m != "" {
		return MintModelPick{Model: m, Knob: KnobExplicit}
	}
	if a.Existed {
		if m := strings.TrimSpace(a.StoredModel); m != "" {
			return MintModelPick{Model: m, Knob: KnobModelResume}
		}
	}
	if !IsFastCheapTask(a.TaskType) {
		return MintModelPick{}
	}
	prov := strings.ToLower(strings.TrimSpace(a.Provider))
	peer := FastCheapModel(a.Portfolio, a.TaskType, prov)
	if peer == "" {
		return MintModelPick{}
	}
	if prov == HarnessCodex && !a.CodexEligible {
		return MintModelPick{
			Knob:   KnobIneligible,
			Reason: "codex weekly ineligible; not pinning " + ModelCodexSpark + " (🎯T390.1.5)",
		}
	}
	if win := ModelContextWindow(peer); win > 0 && a.PromptTokens > win {
		return MintModelPick{
			Knob: KnobEscalated,
			Reason: fmt.Sprintf("prompt %d exceeds %s %d; frontier",
				a.PromptTokens, peer, win),
		}
	}
	return MintModelPick{Model: peer, Knob: KnobFastCheap}
}

// Cite is the start-result fragment for an automatic or explicit model pin.
// Empty when the caller should stay on the provider default with no note.
func (p MintModelPick) Cite() string {
	switch p.Knob {
	case "", KnobModelResume:
		return ""
	}
	s := "model_knob: " + p.Knob
	switch {
	case p.Model != "" && p.Reason != "":
		s += " (" + p.Model + "; " + p.Reason + ")"
	case p.Model != "":
		s += " (" + p.Model + ")"
	case p.Reason != "":
		s += " (" + p.Reason + ")"
	}
	return s
}

// EstimatePromptTokens is a conservative rune/4 count for mint-time
// ineligibility. Not a billed tokenizer; 0 for empty input.
func EstimatePromptTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n <= 0 {
		return 0
	}
	return (n + 3) / 4
}
