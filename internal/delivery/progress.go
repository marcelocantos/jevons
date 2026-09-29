// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package delivery

// Progress types Claudia's sibling checkout emits for an escalation
// ladder (claudia 🎯T138). The published module does not export the
// constants; these spellings are the ones the phase mapper and the
// mid-turn relay already match.
const (
	ProgressDeliveryAbsorbed  = "delivery_absorbed"
	ProgressDeliveryEscalated = "delivery_escalated"
)
