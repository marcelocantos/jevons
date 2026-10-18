// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

// ClassContextOverflow: the provider refused a turn because the seat's
// conversation is longer than the model's context window (🎯T926).
//
// On 2026-09-29 the development overseer reached 1,058,579 tokens against a
// 1,000,000 window and every turn after that was refused with
// `400 invalid_request_error: prompt is too long`. The refusal fell through
// to client_bug, whose copy tells the owner to "fix config, session, or wire
// state" — none of which helps. Claudia's sidecar now compacts a seat before
// it reaches the window and retries a refused turn once after compacting
// (claudia 🎯T148); a refusal that still arrives is terminal: re-sending the
// same seat anything cannot succeed. The remedy is a same-provider remint
// with a thin continue brief (🎯T561), not waiting and not a provider move.
//
// Not transient (re-pressure grows the prompt it refuses) and never a
// hard-block: one seat's long conversation says nothing about whether the
// provider serves the rest of the fleet.
const ClassContextOverflow Class = "context_overflow"

// IsContextOverflow reports whether err carries the context-overflow cause.
// A sentence that only says the prompt is too long does not.
func IsContextOverflow(err error) bool {
	return ProviderCause(err) == CauseContextOverflow
}

func contextOverflowCopy(raw string) string {
	return "The seat's conversation is longer than the model's context window (context_overflow). " +
		"The sidecar compacts a seat before it reaches the window and retries a refused turn once after compacting; " +
		"this refusal means compaction could not bring it back under, so re-sending cannot succeed. " +
		"Not a provider outage. Remint the seat on the same provider with a thin continue brief " +
		"(jevons_agent_kill, then jevons_agent_start). " + detailSuffix(raw)
}
