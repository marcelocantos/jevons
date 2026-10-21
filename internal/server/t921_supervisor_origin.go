// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import "strings"

// 🎯T921: an automated supervisor running outside the fleet posts through
// the owner's HTTP send. Delivery follows the declared origin
// "supervisor". A rendered sentence is not that declaration. A
// supervisor's message travels as an agent note: not the owner's words,
// and never owner urgency.

// originSupervisor is the wire value a supervisor may send.
const originSupervisor = "supervisor"

// supervisorOrigin reports whether origin declares the supervisor. The
// text argument is unused so a rendered sentence cannot decide.
func supervisorOrigin(origin, _ string) (string, bool) {
	if strings.ToLower(strings.TrimSpace(origin)) == originSupervisor {
		return sendOriginAgent, true
	}
	return origin, false
}
