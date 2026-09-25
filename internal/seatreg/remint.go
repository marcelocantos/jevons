// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

import (
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/spool"
)

// RemintSubscription points a fleet seat at the Oh My Pi sidecar (🎯T866.6).
// Claude/Codex ids become anthropic/openai-codex. Grok and Cursor keep
// their fleet names; Launch talks to the sidecar as xai-oauth / cursor.
// A seat with no dated-spool history is dematerialized so RequireResume
// does not look for a vendor JSONL the sidecar will not write.
func RemintSubscription(def *claudia.AgentDef, spoolDir string) bool {
	if def == nil || !subscriptionPlan(def.Provider) {
		return false
	}
	before := *def
	def.Provider = cli.SidecarLaunchProvider(def.Provider)
	if def.Provider == claudia.ProviderGrok {
		def.Provider = claudia.Provider("xai-oauth")
	}
	def.GrokConnect = false
	def.ConnectURL = ""
	def.ConnectPID = 0
	if spoolDir == "" {
		spoolDir = spool.Dir()
	}
	if !spool.SeatHasHistory(spoolDir, def.Name) {
		def.Materialized = false
	}
	return def.Provider != before.Provider ||
		def.Materialized != before.Materialized ||
		def.GrokConnect != before.GrokConnect ||
		def.ConnectURL != before.ConnectURL ||
		def.ConnectPID != before.ConnectPID
}

// RemintRegistry rewrites every subscription-plan row onto the sidecar.
// That is the fleet, not one smoke seat (🎯T866.6).
func RemintRegistry(reg *claudia.Registry, spoolDir string) (int, error) {
	if reg == nil {
		return 0, nil
	}
	n := 0
	for _, row := range reg.List() {
		def := row
		if !RemintSubscription(&def, spoolDir) {
			continue
		}
		if err := reg.Register(def); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SubscriptionPlan is a fleet id whose Launch talks to the sidecar.
func SubscriptionPlan(p claudia.Provider) bool {
	return subscriptionPlan(p)
}

func subscriptionPlan(p claudia.Provider) bool {
	switch p {
	case claudia.ProviderClaude, claudia.ProviderCodex,
		claudia.ProviderGrok, claudia.ProviderCursor,
		claudia.Provider("anthropic"), claudia.Provider("openai-codex"),
		claudia.Provider("xai-oauth"):
		return true
	default:
		return false
	}
}
