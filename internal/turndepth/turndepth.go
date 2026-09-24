// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package turndepth counts in-flight tool calls for a sidecar seat from
// the dated spool. The Claude Code hook that used to own this name is
// gone; sidecar seats have no vendor JSONL for it to read (🎯T866.4).
package turndepth

import "github.com/marcelocantos/jevons/internal/spool"

// Depth is the number of tool_call records for seat that have not been
// closed by a later turn_end. Readers use this instead of a Claude hook.
func Depth(dir, seat string) int {
	recs, err := spool.ReadSeat(dir, seat)
	if err != nil {
		return 0
	}
	depth := 0
	for _, rec := range recs {
		switch rec.Type {
		case "tool_call":
			depth++
		case "turn_end":
			depth = 0
		}
	}
	if depth < 0 {
		return 0
	}
	return depth
}
