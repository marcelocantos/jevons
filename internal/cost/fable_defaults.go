// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// ModelClaudeFableClass is the Claude Fable-class pin documented for 🎯T358.
const ModelClaudeFableClass = "claude-fable-5"

// Fable ratification values (config fable_class_defaults.ratified).
const (
	FableRatifiedKeepCurrent = "keep_current"
	FableRatifiedRootFable   = "root_fable"
	FableRatifiedPOFable     = "po_fable"
	FableRatifiedBoth        = "both"
	FableRatifiedTiered      = "tiered"
)

// KnobFableClassDefault is cited on mint when 🎯T358 ratification pins Fable.
const KnobFableClassDefault = "fable_class_default"

// NormalizeFableRatification maps owner/config text to a known ratification
// or keep_current when empty/unknown.
func NormalizeFableRatification(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "", "pending", "defer", "deferred", FableRatifiedKeepCurrent:
		return FableRatifiedKeepCurrent
	case FableRatifiedRootFable, "root", "overseer", "overseer_fable":
		return FableRatifiedRootFable
	case FableRatifiedPOFable, "po", "pos", "product_owner":
		return FableRatifiedPOFable
	case FableRatifiedBoth, "all":
		return FableRatifiedBoth
	case FableRatifiedTiered:
		return FableRatifiedTiered
	default:
		return FableRatifiedKeepCurrent
	}
}

// FableRatificationPinsRoot reports whether ratification applies Fable to the
// root overseer.
func FableRatificationPinsRoot(ratified string) bool {
	switch NormalizeFableRatification(ratified) {
	case FableRatifiedRootFable, FableRatifiedBoth, FableRatifiedTiered:
		return true
	default:
		return false
	}
}

// FableRatificationPinsPO reports whether ratification applies Fable to
// product-owner mints.
func FableRatificationPinsPO(ratified string) bool {
	switch NormalizeFableRatification(ratified) {
	case FableRatifiedPOFable, FableRatifiedBoth:
		return true
	default:
		return false
	}
}

// FableClassMintModel applies 🎯T358 owner-ratified defaults after explicit
// model= and resume, before fast-cheap and provider-default pins.
func FableClassMintModel(a MintModelArgs) MintModelPick {
	r := NormalizeFableRatification(a.FableRatified)
	if r == FableRatifiedKeepCurrent {
		return MintModelPick{}
	}
	if !claudeHarness(a.Provider) {
		return MintModelPick{}
	}
	role := strings.ToLower(strings.TrimSpace(a.Role))
	want := false
	switch role {
	case "overseer":
		want = FableRatificationPinsRoot(r)
	case "product-owner":
		want = FableRatificationPinsPO(r)
	}
	if !want {
		return MintModelPick{}
	}
	return MintModelPick{
		Model:  ModelClaudeFableClass,
		Knob:   KnobFableClassDefault,
		Reason: "🎯T358 ratified=" + r,
	}
}

func claudeHarness(provider string) bool {
	return seatprovider.Resolve(claudia.Provider(provider)).Plan == seatprovider.Claude
}

// EffectiveOverseerModel returns the model pin for registerOverseerSeat
// when overseer_model/model are empty and 🎯T358 root ratification applies.
func EffectiveOverseerModel(overseerModel, workerModel, ratified, provider string) string {
	if m := strings.TrimSpace(overseerModel); m != "" {
		return m
	}
	if m := strings.TrimSpace(workerModel); m != "" {
		return m
	}
	if !FableRatificationPinsRoot(ratified) || !claudeHarness(provider) {
		return ""
	}
	return ModelClaudeFableClass
}
