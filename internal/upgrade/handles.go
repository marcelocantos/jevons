// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/marcelocantos/claudia"
)

// FromRegistry builds upgrade handles from a live claudia registry. Every
// seat runs on the omp agent core under the claudia broker, which keeps it
// alive across a coordinator restart; the successor reclaims it by name, so
// a handle carries identity only, never a transport.
func FromRegistry(reg *claudia.Registry) []Handle {
	if reg == nil {
		return nil
	}
	defs := reg.List()
	out := make([]Handle, 0, len(defs))
	for _, d := range defs {
		h := Handle{
			Name:      d.Name,
			SessionID: d.SessionID,
			WorkDir:   d.WorkDir,
			Provider:  string(d.Provider),
		}
		if reg.Get(d.Name) != nil && seatstate.ReadRegistry(reg, d.Name).Alive == seatstate.Yes {
			h.Alive = true
		}
		out = append(out, h)
	}
	return out
}
