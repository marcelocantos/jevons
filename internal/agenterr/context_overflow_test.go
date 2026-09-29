// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// incidentRefusal is the refusal the development overseer's every turn got
// on 2026-09-29, as claudia's omp pump published it (🎯T926).
const incidentRefusal = `provider refused the turn: 400 {"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 1058579 tokens > 1000000 maximum"}}`

func TestT926ClassifiesContextOverflow(t *testing.T) {
	t.Parallel()
	cases := []string{
		incidentRefusal,
		// claudia ErrContextOverflow as WaitForResponse wraps it.
		"context overflow: the conversation is longer than the model's window: provider refused the turn: 400 prompt is too long",
		"This model's maximum context length is 128000 tokens. However, your messages resulted in 130000 tokens",
		`400 {"error":{"code":"context_length_exceeded","message":"Your input exceeds the context window of this model."}}`,
		"This model's maximum prompt length is 131072 but the request contains 140000 tokens.",
		"Input is too long for requested model.",
	}
	for _, in := range cases {
		if got := agenterr.ClassifyText(in); got != agenterr.ClassContextOverflow {
			t.Errorf("ClassifyText(%q) = %q, want context_overflow", in, got)
		}
	}
}

func TestT926ContextOverflowIsTerminalNotHardBlock(t *testing.T) {
	t.Parallel()
	// Re-pressure resends a prompt that only grows: never transient.
	if agenterr.TransientBackend(agenterr.ClassContextOverflow) {
		t.Fatal("context_overflow must not be poll-and-retry transient")
	}
	// One seat's long conversation says nothing about the provider serving
	// the rest of the fleet: never a hard-block.
	if agenterr.HardBlock(agenterr.ClassContextOverflow, incidentRefusal) {
		t.Fatal("context_overflow must not enter the fleet hard-block")
	}
}

func TestT926OwnerCopyNamesRemintNotConfig(t *testing.T) {
	t.Parallel()
	got := agenterr.OwnerCopy(agenterr.ClassifyText(incidentRefusal), incidentRefusal)
	for _, want := range []string{"context_overflow", "context window", "jevons_agent_kill", "jevons_agent_start", "1058579"} {
		if !strings.Contains(got, want) {
			t.Errorf("owner copy missing %q: %q", want, got)
		}
	}
	// The old client_bug copy sent the owner to config and wire state.
	if strings.Contains(got, "Fix config") {
		t.Errorf("owner copy still gives the client_bug remedy: %q", got)
	}
}

func TestT926RateLimitsAreNotOverflow(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"429 rate limit: too many tokens per minute",
		"429 usage_limit_reached: weekly limit resets 2026-10-03T23:20Z",
		"400 invalid_request_error: messages: text content blocks must be non-empty",
	} {
		if got := agenterr.ClassifyText(in); got == agenterr.ClassContextOverflow {
			t.Errorf("ClassifyText(%q) = context_overflow, want another class", in)
		}
	}
	// Authored prose that names the incident is never classified (🎯T455).
	if got := agenterr.ClassifyFrom(agenterr.SourceAuthored, incidentRefusal); got != agenterr.ClassNone {
		t.Errorf("authored quote of the refusal classified as %q", got)
	}
}
