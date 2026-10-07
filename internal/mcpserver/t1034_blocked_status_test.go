// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "testing"

// Incident: a finish-shaped progress report ended with GOAL_STATUS: blocked
// and a question for the PO. The old typed-finish shortcut reaped it.
const t1034Incident = "```jevons\njevons: kind finish-report\njevons: target T1034\njevons: status in-progress\njevons: oracle-sha abcdef0123456789\njevons: silent-ledger none\n```\nThe implementation is done locally; SHA abcdef0123456789. GOAL_STATUS: blocked\nCan the PO decide whether to activate this before I continue?\nGOAL_STATUS: blocked"

func TestT1034BlockedGoalAndInProgressDoNotReap(t *testing.T) {
	for _, tc := range []struct {
		name, report string
		action       WorkerIdleAction
	}{
		{"incident", t1034Incident, IdleActionPark},
		{"in-progress field despite done prose", "```jevons\njevons: kind finish-report\njevons: target T1034\njevons: status in-progress\njevons: oracle-sha abcdef0123456789\njevons: silent-ledger none\n```\nDone. SHA abcdef0123456789.", IdleActionKeep},
		{"blocked goal without envelope", "Done. Nothing left for this worker. SHA abcdef0123456789.\nGOAL_STATUS: blocked", IdleActionPark},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyWorkerIdleDisposition(tc.report); got != tc.action {
				t.Fatalf("disposition = %s want %s", got, tc.action)
			}
			if LooksLikeFinishedWorkReport(tc.report) {
				t.Fatal("finished-work heuristic overrode open status")
			}
			reg := t439Registry(t, "jv-t1034-specimen")
			if ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t1034-specimen", tc.report, nil); ok {
				t.Fatalf("reaped as %s", reason)
			}
			d := ClassifyStopDisposition("", "", tc.report, false, false, false)
			if tc.action == IdleActionPark && d.Action != IdleActionPark {
				t.Fatalf("stop disposition = %s", d.Action)
			}
		})
	}
}

func TestT1034CitedBlockedGoalDoesNotVetoGenuineFinish(t *testing.T) {
	report := "Done. SHA abcdef0123456789; go test ./internal/mcpserver PASS.\n```\nGOAL_STATUS: blocked\n```"
	if got := ClassifyWorkerIdleDisposition(report); got != IdleActionReap {
		t.Fatalf("quoted marker yielded %s", got)
	}
}
