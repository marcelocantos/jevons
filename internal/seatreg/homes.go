// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

// Jevons owns its fleet registry, tools, and conversation log. Claudia
// owns the shared host broker; Jevons remints its fleet on daemon boot
// (🎯T875).
const (
	HomeRegistry        = "internal/seatreg"
	HomeToolBodies      = "internal/mcpserver"
	HomeConversationLog = "internal/spool"
)
