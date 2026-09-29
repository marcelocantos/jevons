// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import "strings"

// 🎯T921: an automated supervisor running outside the fleet (the owner's
// overnight Codex thread) posts through the owner's HTTP send with no
// origin, so its passes were recorded as the owner's: owner bubbles in the
// cockpit, owner barriers that split a streaming reply, owner urgency on
// delivery. It says what it is — "Automated supervisor pass, not the
// owner", "no authority claimed" — and a message that declares itself not
// the owner's is not recorded as the owner's. An explicit origin
// "supervisor" says the same thing without the wording.

// originSupervisor is the wire value a supervisor may send.
const originSupervisor = "supervisor"

// supervisorOrigin maps a send's declared origin and text to the origin it
// is delivered under, and reports whether it is a supervisor's. A
// supervisor's message travels as an agent note: not the owner's words, and
// never owner urgency.
func supervisorOrigin(origin, text string) (string, bool) {
	if strings.EqualFold(origin, originSupervisor) {
		return sendOriginAgent, true
	}
	if (origin == "" || origin == sendOriginOwner) && declaresNotOwner(text) {
		return sendOriginAgent, true
	}
	return origin, false
}

// declaresNotOwner reports a message that says, at its start, that it is
// not the owner's.
func declaresNotOwner(text string) bool {
	head := strings.ToLower(strings.TrimSpace(text))
	if len(head) > 240 {
		head = head[:240]
	}
	return strings.HasPrefix(head, "automated supervisor") ||
		strings.Contains(head, "not the owner") ||
		strings.Contains(head, "no authority claimed")
}
