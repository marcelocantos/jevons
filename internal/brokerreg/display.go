// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package brokerreg

import (
	"fmt"
	"strings"
)

// FormatRegistryAbsentSection names broker-held cursor seats missing from the
// registry. Empty when there is nothing to report. Control surfaces may call
// this on [RegistryAbsentRow] values produced by the observation feed.
func FormatRegistryAbsentSection(rows []RegistryAbsentRow) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Claudia-held cursor seats not in jevons registry (🎯T1174):\n")
	for _, seat := range rows {
		fmt.Fprintf(&b, "  %-20s provider=%s broker_alive=true  (no registry row — 🎯T412/🎯T444 do not apply)\n",
			seat.Name, seat.Provider)
	}
	return strings.TrimRight(b.String(), "\n")
}
