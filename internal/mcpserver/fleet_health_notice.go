// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// FleetHealthNoticeClass is declared at each notifyFleetHealth call site (🎯T1158).
// The class is not inferred from the notice's English.
type FleetHealthNoticeClass string

const (
	// noticeInform is written to the event log only; it does not deliver to the
	// overseer or render in the owner's main chat.
	noticeInform FleetHealthNoticeClass = "inform"
	// noticeAct reports a fault, asks for a decision, or names a recoverable hold
	// the overseer can act on — delivery is unchanged.
	noticeAct FleetHealthNoticeClass = "act"
)

func fleetHealthClassValid(c FleetHealthNoticeClass) bool {
	return c == noticeInform || c == noticeAct
}
