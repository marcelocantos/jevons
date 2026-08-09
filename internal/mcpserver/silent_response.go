// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "github.com/marcelocantos/jevons/internal/silentresponse"

// SilentResponsePrefix marks fleet-ops replies that must not surface to the
// owner (worker notes / overseer chat). Ops events (daemon-restarted,
// worker-idle) instruct agents to use this when no owner decision is needed.
// Canonical definition: internal/silentresponse (shared with overseer→owner
// chat wire, 🎯T238).
const SilentResponsePrefix = silentresponse.Prefix

// silentResponseInstruction is appended to ops event bodies so POs know how
// to mark filterable replies (and when not to).
const silentResponseInstruction = "" +
	"\nRESPONSE RULE (mandatory):\n" +
	"- If you only status-check / resume workers and the owner needs no decision, " +
	"your entire reply MUST start with exactly [silent] (case-insensitive on the filter).\n" +
	"  Example: [silent] continued jv-t212; 🎯T222 already working.\n" +
	"- Do NOT write owner-facing \"everything is fine\" chatter without [silent].\n" +
	"- If you need an owner decision or something is broken beyond your tools, " +
	"do NOT use [silent] — escalate normally.\n" +
	"- Prefer acting (continue / re-brief / restart workers) over narrating health.\n"

// IsSilentAgentResponse reports whether agent completion text is owner-filterable.
// Same predicate as silentresponse.Is (worker→overseer notify + overseer→owner wire).
func IsSilentAgentResponse(text string) bool {
	return silentresponse.Is(text)
}
