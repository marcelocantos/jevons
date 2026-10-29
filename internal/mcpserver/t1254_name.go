// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"unicode"
)

// singleSeatName rejects a list accidentally supplied as one fleet address.
// An existing reaped stamp is an address too, but an arbitrary parked intent
// is not evidence that a seat was ever registered.
func (s *Server) singleSeatName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("agent name is required")
	}
	if invalidSingleSeatName(name) {
		return fmt.Errorf("invalid agent name %q: whitespace is not allowed (pass one name)", name)
	}
	if s.registry != nil && s.registry.Def(name) != nil {
		return nil
	}
	if _, ok := LookupReapedRecord(s.fleetIntent(), name); ok {
		return nil
	}
	return fmt.Errorf("agent %q is not registered or reaped", name)
}

func invalidSingleSeatName(name string) bool { return strings.IndexFunc(name, unicode.IsSpace) >= 0 }
