// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

// relayedSteerRefusalReport is the broker's rendered steer refusal, the same
// wording the send path already classifies with escalate.SteerUnsupported.
// It is an error string, not a terminal work report.
const relayedSteerRefusalReport = "broker protocol: agent_failed: steer unsupported: claude provider: provider has no steer mechanism (policy queue_until_idle)"

// t165ProseFinish is the standing 🎯T165 prose finish. A steer-refusal
// mention must not strip this shape.
const t165ProseFinish = "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS"

// 🎯T766: a rendered steer refusal is not finished work. The prose finish
// contracts stay: a bare claim, an oracle finish, and a finish that merely
// quotes the refusal still reap. The reap path does not consult the steer
// sentinel; these strings are not completion claims.
func TestT766SteerRefusalIsNotFinishedWork(t *testing.T) {
	reg := regWithTree(t)
	isWork := func(string) bool { return false }

	refusals := []string{
		relayedSteerRefusalReport,
		"steer unsupported",
		"steer unsupported: but not relayed and not the sentinel",
	}
	for _, report := range refusals {
		ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", report, isWork)
		if ok {
			t.Errorf("rendered refusal reaped (%s): %q", reason, report)
		}
		if isFinishReapReason(reason) {
			t.Errorf("rendered refusal reason %q is a finish: %q", reason, report)
		}
	}

	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", t165ProseFinish, isWork)
	if !ok {
		t.Fatalf("prose oracle finish must still reap; reason %s", reason)
	}
	if reason != "finished_work" {
		t.Fatalf("prose oracle finish reason = %q", reason)
	}

	quoted := t165ProseFinish + ". The log quoted \"steer unsupported\"."
	ok, reason = ShouldAutoReapDoneWorkAgent(reg, "worker", quoted, isWork)
	if !ok {
		t.Fatalf("quoting the refusal inside a prose finish must still reap; reason %s", reason)
	}
	if reason != "finished_work" {
		t.Fatalf("quoted-mention finish reason = %q", reason)
	}
	if strings.Contains(reason, "steer") {
		t.Fatalf("reap reason must not be the refusal text: %s", reason)
	}
}

func isFinishReapReason(reason string) bool {
	switch reason {
	case "finished_work", "bare_done_completion", "accepted_risk", ownWorkCompleteReapReason:
		return true
	default:
		return false
	}
}
