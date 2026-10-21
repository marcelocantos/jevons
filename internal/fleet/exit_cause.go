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

// BrokerCaused reports that cause is one of the published broker exit
// codes. A sentence that only begins with the broker's words is not.
func BrokerCaused(cause string) bool {
	return cause == ExitCauseBrokerLost || cause == ExitCauseBrokerRestarted
}
