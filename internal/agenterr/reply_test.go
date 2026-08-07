// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import (
	"strings"
	"testing"
)

// 🎯T283: a reply that is nothing but a provider failure classifies, so the
// MCP direct/deliver paths can report an outage instead of a product defect.
func TestClassifyReplyFiresOnBareProviderFailure(t *testing.T) {
	cases := []struct {
		reply string
		want  Class
	}{
		{"Internal error", ClassBackendUnavailable},
		{"  Internal error.  ", ClassBackendUnavailable},
		{"acp session/prompt: Internal error", ClassBackendUnavailable},
		{"Error: 503 Service Unavailable", ClassBackendUnavailable},
		{"429 Too Many Requests", ClassRateLimit},
		{"401 Unauthorized: invalid api key", ClassAuth},
	}
	for _, c := range cases {
		if got := ClassifyReply(c.reply); got != c.want {
			t.Errorf("ClassifyReply(%q) = %v, want %v", c.reply, got, c.want)
		}
	}
}

// The J10 shape: an outage arrives as the whole turn where a shell marker was
// expected. It must classify as backend_unavailable, not read as a tool that
// silently failed to run.
func TestClassifyReplyJ10OutageTurn(t *testing.T) {
	class := ClassifyReply("Internal error")
	if class != ClassBackendUnavailable {
		t.Fatalf("class = %v, want backend_unavailable", class)
	}
	if !class.IsTransient() {
		t.Error("backend_unavailable must be transient so recovery re-pressures")
	}
	_, ownerMsg, ok := ReplyFailure("Internal error")
	if !ok {
		t.Fatal("ReplyFailure must report the outage")
	}
	if !strings.Contains(ownerMsg, "backend_unavailable") {
		t.Errorf("owner copy must name the class, got %q", ownerMsg)
	}
}

// The hazard this guard exists for: work prose contains failure words. The
// owner chat path classifies single streamed events, but a direct/deliver
// caller sees the whole aggregated turn, where ClassifyText would rewrite
// honest results.
func TestClassifyReplyIgnoresWorkProse(t *testing.T) {
	replies := []string{
		"Done — fixed the timeout bug in fleet.go.",
		"I fixed the auth error handling and added a test.",
		"Completed: retries now back off on 503 responses.",
		"Wrote the rate limit guard; tests passing.",
		"J10_SHELL_OK",
		"The connection refused path is now covered by a unit test — committed as abc1234.",
		strings.Repeat("Analysis of the internal error handling. ", 20),
		"error", // bare residual word is not proof of an outage
		"Something went wrong somewhere",
	}
	for _, r := range replies {
		if got := ClassifyReply(r); got.IsFailure() {
			t.Errorf("ClassifyReply(%q) = %v, want none (real work must not be rewritten)", r, got)
		}
	}
}

// Busy is a queueing signal, not a provider failure — parity with Classify.
func TestClassifyReplyBusyIsNotFailure(t *testing.T) {
	if got := ClassifyReply("prompt already in flight"); got.IsFailure() {
		t.Errorf("busy classified as %v, want none", got)
	}
}

// Long or multi-paragraph output is a worker doing its job, whatever words it
// happens to use.
func TestClassifyReplyRejectsLongOutput(t *testing.T) {
	long := "Internal error handling review:\n" +
		strings.Repeat("line about service unavailable retries\n", 10)
	if got := ClassifyReply(long); got.IsFailure() {
		t.Errorf("long output classified as %v, want none", got)
	}
	if got := ClassifyReply(""); got.IsFailure() {
		t.Errorf("empty classified as %v, want none", got)
	}
}

func TestReplyFailureOkFalseOnWork(t *testing.T) {
	class, msg, ok := ReplyFailure("Done — shell ran, marker written.")
	if ok {
		t.Fatalf("ok=true for work reply (class=%v msg=%q)", class, msg)
	}
	if class != ClassNone || msg != "" {
		t.Errorf("class=%v msg=%q, want zero values", class, msg)
	}
}
