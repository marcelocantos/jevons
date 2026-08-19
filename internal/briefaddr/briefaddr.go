// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package briefaddr binds a 🎯T425 identity header to the seat it may enter.
//
// THE INCIDENT, 2026-08-15 (🎯T452). A post-restart brief composed for
// jv-t443-red-as-proof — ROLE: work agent, PARENT: jevons-po, TARGET: 🎯T443 —
// arrived as a queued_command in the OVERSEER's composer. Every composition
// was right: the daemon read each recipient's registry row and wrote that row
// into the message it was about to send. What went wrong was between the
// composing and the arriving.
//
// WHY A STRAY MESSAGE IS NOT THE RIGHT NAME FOR IT. 🎯T425 exists so no agent
// has to consult the registry to learn its own name, and it says so in the
// second person: "these lines are the registry's own answer, and they are the
// one you act on." Delivered into the wrong seat, the anti-confusion
// instrument becomes an instruction to impersonate — a name, a parent, a
// bound target and the role doctrine of a role the reader does not occupy.
//
// SO THE TEXT AND THE DESTINATION ARE BOUND. The header is not decoration a
// delivery path may ignore; it is an assertion about who is receiving this
// message, and every point that still holds both the payload and the seat is
// a point where that assertion can be checked. The mcpserver send path checks
// it outbound (🎯T452); the overseer chat wire checks it inbound (🎯T513).
// This package is the one definition both check against.
package briefaddr

import (
	"fmt"
	"strings"
)

// Marker opens a 🎯T425 identity header. mcpserver writes it; every
// addressing check anchors on it.
const Marker = "[Who you are"

// nameLabel is the line the header writes the recipient's durable name on.
const nameLabel = "- NAME: "

// IdentityHeaderName reports the name a 🎯T425 identity header claims for its
// reader, or "" when text carries no header.
//
// The answer is taken from the header BLOCK only — the marker line the message
// OPENS with, and the "- LABEL: value" lines that immediately follow it.
// Everything below is the brief, and the brief quotes agent names freely (a
// worker pasting a header it received quotes a "- NAME: " line verbatim). A
// parser that scanned the whole message would let the body answer a question
// only the header may answer.
//
// Anchored at the start for the same reason, and it matters more than it
// looks: the header is only ever written at position 0, so a marker deeper in
// the body belongs to a quotation. The message most likely to quote a whole
// header is the report 🎯T452 asks a misdelivered-to agent to send, and
// answering with the quoted name would have the send path refuse the one
// message that describes the incident.
func IdentityHeaderName(text string) string {
	body := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(body, Marker) {
		return ""
	}
	lines := strings.Split(body, "\n")
	for n, line := range lines {
		if n == 0 {
			continue // the marker line itself
		}
		if name, ok := strings.CutPrefix(line, nameLabel); ok {
			return strings.TrimSpace(name)
		}
		if !strings.HasPrefix(line, "- ") {
			return "" // end of the header block; the NAME line was not in it
		}
	}
	return ""
}

// MisaddressedBriefError is what a delivery is refused with when the payload's
// identity header names an agent other than the seat it resolved to.
//
// A distinct type rather than fmt.Errorf so the daemon can recognise its own
// refusal — the difference between "this send failed" and "this send was about
// to put one agent's identity into another agent's session" is the difference
// between a retry and an incident.
type MisaddressedBriefError struct {
	// Addressed is the name the identity header claims for its reader.
	Addressed string
	// Destination is the seat the delivery resolved to.
	Destination string
}

func (e *MisaddressedBriefError) Error() string {
	return fmt.Sprintf(
		"refusing to deliver: this message carries a 🎯T425 identity header naming %q, "+
			"but the delivery resolved to %q's seat. A brief that names its reader is an "+
			"instruction to be that agent, so delivering it here would hand %[2]q another "+
			"agent's name, parent and target (🎯T452). Nothing was delivered. The likely "+
			"cause is a stale or rotated session id resolving %[1]q to the wrong pane — "+
			"check both registry rows' session ids against the live processes",
		e.Addressed, e.Destination)
}

// Check returns a *MisaddressedBriefError when text carries an identity
// header naming an agent other than dest.
//
// A message with no header is not this check's business: relayed digests,
// owner turns and worker replies carry no claim about who the reader is, and
// inventing one here would be the same defect from the other side. Comparison
// is case-insensitive because delivery resolves names that way.
func Check(dest, text string) error {
	addressed := IdentityHeaderName(text)
	if addressed == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(addressed), strings.TrimSpace(dest)) {
		return nil
	}
	return &MisaddressedBriefError{Addressed: addressed, Destination: strings.TrimSpace(dest)}
}
