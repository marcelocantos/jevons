// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
)

// registerOverseerSeat ensures the overseer's registry row on every boot,
// first or restart, and stores the provider id Launch talks to (🎯T1077).
//
// Worker mints have stored the sidecar seat id since f0c3f7bd (grok →
// xai-oauth, claude → anthropic, codex → openai-codex): Claudia runs every
// one of those plans on the omp sidecar, and Jevons' sidecar branches
// (spool.SidecarProvider) recognise the seat only by that id. The overseer
// kept the plan id from config. Its records were in the spool all along,
// but the owner-send payload watch never confirmed a turn (so Materialized
// was never promoted), and after a restart the transcript observer had no
// turn-began, no materialized flag and no evidence it would read, so the
// alive overseer read phase_unknown until someone sent it a message. Gate
// 4c638044 lost J44, J39 and ~22 journeys behind them to that.
//
// A row an older boot stored as a plan id is normalized here too, which is
// what lets a running development overseer recover on its next restart.
func registerOverseerSeat(registry *claudia.Registry, name, dir, model string, defaultProvider claudia.Provider) (*claudia.AgentDef, error) {
	def, err := registry.EnsureAgent(name, dir, model, true)
	if err != nil {
		return nil, err
	}
	// 🎯T148: keep the stored provider on resume; only set the default when
	// empty. The plan is kept; only its spelling becomes the seat id.
	def.Provider = cli.SubscriptionSeatProvider(cli.SelectAgentProvider("", def.Provider, defaultProvider))
	// 🎯T114: overseer purpose on the unified participant record.
	if def.Purpose == "" {
		def.Purpose = claudia.PurposeOverseer
	}
	// Overseer MCP registration happens after the listener is bound, so the
	// advertised URL names the port actually served (🎯T379).
	if err := registry.Register(*def); err != nil {
		return nil, err
	}
	return def, nil
}
