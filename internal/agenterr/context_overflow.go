// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

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

// contextOverflowMarkers are the providers' own phrasings of the refusal,
// plus claudia's ErrContextOverflow text and its Event.Reason code. Taken
// from pi-ai's context-overflow evidence patterns, keeping only phrases
// that cannot be read as a rate limit ("too many tokens" per minute is one).
var contextOverflowMarkers = []string{
	"prompt is too long",                    // Anthropic
	"input is too long for requested model", // Amazon Bedrock
	"exceeds the context window",            // OpenAI
	"maximum prompt length is",              // xAI
	"maximum context length is",             // OpenRouter
	"context_length_exceeded",               // OpenAI error code
	"context length exceeded",               // generic
	"model_context_window_exceeded",         // z.ai
	"context overflow",                      // claudia ErrContextOverflow
	"context_overflow",                      // claudia Event.Reason
}

// IsContextOverflow reports whether msg is a provider's refusal of a prompt
// longer than the model's context window.
func IsContextOverflow(msg string) bool {
	return containsAny(strings.ToLower(msg), contextOverflowMarkers...)
}

func contextOverflowCopy(raw string) string {
	return "The seat's conversation is longer than the model's context window (context_overflow). " +
		"The sidecar compacts a seat before it reaches the window and retries a refused turn once after compacting; " +
		"this refusal means compaction could not bring it back under, so re-sending cannot succeed. " +
		"Not a provider outage. Remint the seat on the same provider with a thin continue brief " +
		"(jevons_agent_kill, then jevons_agent_start). " + detailSuffix(raw)
}
