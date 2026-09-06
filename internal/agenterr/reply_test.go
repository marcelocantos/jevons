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

func TestNumericStatusFragmentsAreNotFailures(t *testing.T) {
	for _, classify := range []struct {
		name string
		fn   func(string) Class
	}{{"reply", ClassifyReply}, {"transport text", ClassifyText}} {
		t.Run(classify.name, func(t *testing.T) {
			for _, code := range []string{"400", "401", "402", "403", "429", "500", "502", "503", "504"} {
				for _, text := range []string{
					"reference" + code, code + "reference", "ref_" + code,
					"ref-" + code + "-id", "1" + code + "9", "字" + code,
					"0." + code, "." + code, "/records/" + code, code + ".json", "status " + code + "9",
				} {
					if got := classify.fn(text); got.IsFailure() {
						t.Errorf("%q classified as %s", text, got)
					}
					if code == "402" && HardBlock(ClassRateLimit, text) {
						t.Errorf("%q manufactured a billing hard-block", text)
					}
				}
			}
			const observed = "orch-direct-1271abc2-9bb6-4700-981b-ecc1500100dd"
			if got := classify.fn(observed); got.IsFailure() {
				t.Errorf("observed successful reply classified as %s", got)
			}
			for _, tc := range []struct {
				code string
				want Class
			}{{"400", ClassClientBug}, {"401", ClassAuth}, {"402", ClassRateLimit}, {"403", ClassAuth},
				{"429", ClassRateLimit}, {"500", ClassBackendUnavailable},
				{"502", ClassBackendUnavailable}, {"503", ClassBackendUnavailable}, {"504", ClassBackendUnavailable}} {
				for _, text := range []string{tc.code, "HTTP " + tc.code, "status=" + tc.code,
					"status: " + tc.code + ".", "HTTP/1.1 " + tc.code,
					"ref-" + tc.code + " HTTP " + tc.code} {
					if got := classify.fn(text); got != tc.want {
						t.Errorf("%q classified as %s, want %s", text, got, tc.want)
					}
					if tc.code == "402" && !HardBlock(tc.want, text) {
						t.Errorf("real billing status %q lost its hard-block", text)
					}
				}
			}
		})
	}
}
