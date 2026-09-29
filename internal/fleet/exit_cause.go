// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import "strings"

// brokerCausePrefix begins every exit cause Claudia gives a broker-backed
// handle: its connection lost, or the broker reporting the seat gone.
const brokerCausePrefix = "claudia broker"

// Exit causes the published claudia module does not export. These are the
// spellings BrokerCaused and BrokerPlanned already match.
const (
	ExitCauseBrokerLost      = "claudia broker connection closed"
	ExitCauseBrokerRestarted = "claudia broker stopped on purpose (planned restart)"
)

// ExitCause is why a dead handle died, when its harness knew (🎯T925). It
// reads a Claudia that exposes the cause and is empty on one that does not,
// so jevons still builds against the published Claudia pin.
func ExitCause(proc any) string {
	c, ok := proc.(interface{ ExitCause() string })
	if !ok || c == nil {
		return ""
	}
	return strings.TrimSpace(c.ExitCause())
}

// BrokerPlanned reports that the broker said it was stopping on purpose
// before it closed the seat's connection: a planned restart (🎯T944).
func BrokerPlanned(cause string) bool {
	return strings.Contains(cause, "stopped on purpose")
}

// BrokerCaused reports that cause is the broker's doing — it stopped,
// restarted or dropped the seat — rather than the seat's own exit.
func BrokerCaused(cause string) bool {
	return strings.HasPrefix(cause, brokerCausePrefix)
}
