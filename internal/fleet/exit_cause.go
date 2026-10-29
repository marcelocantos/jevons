// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// Exit causes, now published by claudia (🎯T1008) as
// claudia.ExitCauseBrokerLost / claudia.ExitCauseBrokerRestarted.
// Aliased here so existing callers keep one import path.
const (
	ExitCauseBrokerLost      = claudia.ExitCauseBrokerLost
	ExitCauseBrokerRestarted = claudia.ExitCauseBrokerRestarted
	// ExitCauseBrokerSeatGonePrefix is how claudia marks a broker-backed
	// handle the broker reported gone while the broker stayed up (sidecar
	// exit, process exit, …). See claudia 🎯T925 / broker_agent.go.
	ExitCauseBrokerSeatGonePrefix = "claudia broker: seat gone:"
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

// BrokerPlanned reports that cause is the published planned-restart code.
// A sentence that only says the broker stopped on purpose is not that code.
func BrokerPlanned(cause string) bool {
	return cause == ExitCauseBrokerRestarted
}

// BrokerSeatGone reports that cause is the broker's seat-gone reason
// (prefix + detail). A sentence that only quotes "seat process gone" is
// not this code.
func BrokerSeatGone(cause string) bool {
	return strings.HasPrefix(cause, ExitCauseBrokerSeatGonePrefix)
}

// BrokerCaused reports that cause is one of the published broker exit
// codes, including seat-gone while the broker stayed up (🎯T1139). A
// sentence that only begins with the broker's words is not.
func BrokerCaused(cause string) bool {
	return cause == ExitCauseBrokerLost || cause == ExitCauseBrokerRestarted || BrokerSeatGone(cause)
}
