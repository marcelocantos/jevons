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

// 🎯T766: a rendered steer refusal is not finished work. 🎯T766.4: neither
// is unenveloped prose. A declared finish-report reaps, including one whose
// payload quotes the refusal. The reap path does not consult the steer
// sentinel.
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

	prose := "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS"
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "worker", prose, isWork)
	if ok {
		t.Fatalf("unenveloped prose reaped (%s)", reason)
	}
	if reason != "unknown_report" {
		t.Fatalf("unenveloped prose reason = %q, want unknown_report", reason)
	}

	quoted := declaredFinishReport("T165", prose+". The log quoted \"steer unsupported\".")
	ok, reason = ShouldAutoReapDoneWorkAgent(reg, "worker", quoted, isWork)
	if !ok {
		t.Fatalf("quoting the refusal inside a declared finish-report must still reap; reason %s", reason)
	}
	if reason != "finished_work" {
		t.Fatalf("quoted-mention finish reason = %q", reason)
	}
	if strings.Contains(reason, "steer") {
		t.Fatalf("reap reason must not be the refusal text: %s", reason)
	}
}

// maybeReapDoneWorkAgent is the notify path. A rendered refusal stays
// registered. Unenveloped prose stays. A declared finish-report, including
// one whose payload quotes the refusal, leaves.
func TestT766SteerRefusalStaysThroughNotifyReap(t *testing.T) {
	refusals := []string{
		relayedSteerRefusalReport,
		"steer unsupported",
		"steer unsupported: but not relayed and not the sentinel",
	}
	for _, report := range refusals {
		reg := t439Registry(t, "jv-t766-steer")
		s := &Server{registry: reg}
		s.maybeReapDoneWorkAgent("jv-t766-steer", report)
		if reg.Def("jv-t766-steer") == nil {
			t.Errorf("notify reap removed a steer refusal: %q", report)
		}
	}

	prose := "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T165 PASS"
	reg := t439Registry(t, "jv-t766-finish")
	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent("jv-t766-finish", prose)
	if reg.Def("jv-t766-finish") == nil {
		t.Fatal("unenveloped prose was reaped on the notify path")
	}

	reg = t439Registry(t, "jv-t766-declared")
	s = &Server{registry: reg}
	s.maybeReapDoneWorkAgent("jv-t766-declared", declaredFinishReport("T165", prose))
	if reg.Def("jv-t766-declared") != nil {
		t.Fatal("declared finish-report stayed registered on the notify reap")
	}

	quoted := declaredFinishReport("T165", prose+". The log quoted \"steer unsupported\".")
	reg = t439Registry(t, "jv-t766-quoted")
	s = &Server{registry: reg}
	s.maybeReapDoneWorkAgent("jv-t766-quoted", quoted)
	if reg.Def("jv-t766-quoted") != nil {
		t.Fatal("a declared finish that quotes the refusal stayed registered")
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
