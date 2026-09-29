// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"regexp"
)

// anthropicToolName is the pattern the Anthropic API holds every custom tool
// name to (🎯T908). A seat harness that passes MCP names through verbatim
// sends jevonsmcp's names as they are, with only a `_` prefix, so one tool
// outside it gets every turn of every Claude seat refused with a 400 — on
// 2026-09-29 that was `self_test.run` and `self_test.list`.
var anthropicToolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// seatToolNamePrefix is what that harness puts in front of an MCP tool name.
const seatToolNamePrefix = "_"

// validToolName reports whether name reaches the API intact, prefix included.
func validToolName(name string) bool {
	return anthropicToolName.MatchString(seatToolNamePrefix + name)
}

// refuseInvalidToolName keeps a bad name off tools/list. One unlisted tool
// costs its callers; one listed tool with a bad name costs every Claude seat
// every turn. The ratchet in tool_name_test.go is what stops one landing.
func refuseInvalidToolName(name string) bool {
	if validToolName(name) {
		return false
	}
	slog.Error("mcp tool not registered: name outside the Anthropic tool-name pattern (T908)",
		"tool", name, "pattern", anthropicToolName.String())
	return true
}
