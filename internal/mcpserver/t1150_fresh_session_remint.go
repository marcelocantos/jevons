// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// rotateSessionOnDeadRemint mints a new session id before launch when a dead
// seat is being started again. A preserved registry row after a reaped kill
// (🎯T560) or a 🎯T1148 provider re-placement must not inherit the prior
// grant's session — especially across providers (🎯T1150).
func rotateSessionOnDeadRemint(def *claudia.AgentDef, storedProvider string, localAlive seatstate.Tri) {
	if def == nil || localAlive == seatstate.Yes {
		return
	}
	if strings.TrimSpace(def.SessionID) == "" {
		return
	}
	// Same-provider dead retry and materialized resume keep the session id
	// (🎯T1046 / 🎯T215). Fresh session is for cross-provider remint (🎯T1150).
	if seatprovider.Same(claudia.Provider(storedProvider), def.Provider) {
		return
	}
	next := fleet.RehydratedDef(*def, uuid.NewString())
	*def = next
}
