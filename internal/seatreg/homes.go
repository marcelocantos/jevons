// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

// T866.7 homes that must live in this repo (not in claudia as the
// product owner). The seat broker is cmd/jevons-broker — a separate
// process — so a jevonsd bounce still reclaims seats by name.
const (
	HomeRegistry        = "internal/seatreg"
	HomeBroker          = "cmd/jevons-broker"
	HomeToolBodies      = "internal/mcpserver"
	HomeConversationLog = "internal/spool"
)
