// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/briefaddr"
)

// Brief addressing (🎯T452 / 🎯T513).
//
// The parsing and the refusal live in internal/briefaddr — one definition of
// "who does this header name" shared with the overseer chat wire, which runs
// the mirror-image check on arrival (internal/server, 🎯T513). The incident
// history and the anchoring rationale are documented on that package. What
// stays here is the send-path wiring: deliverByName refuses a payload whose
// header names an agent other than the seat the delivery resolved to.

// IdentityHeaderName reports the name a 🎯T425 identity header claims for its
// reader, or "" when text carries no header. See briefaddr.IdentityHeaderName.
func IdentityHeaderName(text string) string {
	return briefaddr.IdentityHeaderName(text)
}

// MisaddressedBriefError is what a send is refused with when the payload's
// identity header names an agent other than the seat it resolved to.
type MisaddressedBriefError = briefaddr.MisaddressedBriefError

// overseerSeatName names the seat the overseer arm of deliverByName delivers
// into — the owner-chat session — as opposed to whatever name a caller
// addressed to get there.
//
// The registry's own answer first, so a renamed overseer is named correctly,
// and the conventional name when no row declares itself. Both are properties of
// the FLEET, never of the message being delivered: the whole point is to have a
// destination that a misaddressed payload can be compared against.
func (s *Server) overseerSeatName() string {
	if s != nil && s.registry != nil {
		for _, d := range s.registry.List() {
			if strings.EqualFold(strings.TrimSpace(d.Purpose), claudia.PurposeOverseer) {
				if n := strings.TrimSpace(d.Name); n != "" {
					return n
				}
			}
		}
	}
	return s.overseerName()
}

// CheckBriefAddressing returns a *MisaddressedBriefError when text carries an
// identity header naming an agent other than dest. See briefaddr.Check.
func CheckBriefAddressing(dest, text string) error {
	return briefaddr.Check(dest, text)
}
