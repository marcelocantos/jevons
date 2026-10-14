// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// brokerCausePrefix begins every exit cause Claudia gives a broker-backed
// handle: its connection lost, or the broker reporting the seat gone.
const brokerCausePrefix = "claudia broker"

// Exit causes, now published by claudia (🎯T1008) as
// claudia.ExitCauseBrokerLost / claudia.ExitCauseBrokerRestarted.
// Aliased here so existing callers keep one import path.
const (
	ExitCauseBrokerLost      = claudia.ExitCauseBrokerLost
	ExitCauseBrokerRestarted = claudia.ExitCauseBrokerRestarted
)

// ExitCause is why a dead handle died, when its harness knew (🎯T925).
// The published claudia.Agent now exports ExitCause() directly
// (🎯T1008); this reads it through an interface so jevons still builds
// against any Claudia that happens not to expose it on a given handle.
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
