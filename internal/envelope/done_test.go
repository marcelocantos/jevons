// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "testing"

func TestClassifyDoneEnvelopeOnly(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want DoneClass
	}{
		{"empty", "", DoneUnknown},
		{"bare done prose", "Done. Mission complete.", DoneUnknown},
		{"oracle prose without fence", "Done. SHA abcdef0123456. GATE x GREEN", DoneUnknown},
		{
			"valid finish-report",
			Format(&Message{
				Kind: KindFinishReport, Target: "T766.4", SHA: "abcdef0123456",
				SilentLedger: SilentLedgerEmpty, Payload: "Landed.",
			}),
			DoneYes,
		},
		{
			"finish-report accepted-risk",
			Format(&Message{
				Kind: KindFinishReport, Target: "T766.4", Risk: RiskClass3,
				SilentLedger: SilentLedgerEmpty, Payload: "Owner smoke residual.",
			}),
			DoneYes,
		},
		{
			"finish-report in-progress",
			Format(&Message{
				Kind: KindFinishReport, Target: "T766.4", SHA: "abcdef0123456",
				Status: ProgressInProgress, SilentLedger: SilentLedgerEmpty,
				Payload: "Slice oracle green; remaining work named.",
			}),
			DoneNo,
		},
		{
			"finish-report blocked",
			Format(&Message{
				Kind: KindFinishReport, Target: "T766.4", Status: ProgressBlocked,
				Blocker: "owner-go-ahead", SilentLedger: SilentLedgerEmpty,
			}),
			DoneNo,
		},
		{
			"scout-report",
			Format(&Message{
				Kind: KindScoutReport, Target: "T766.4",
				SilentLedger: SilentLedgerEmpty,
				Payload: "Scout done. Fog mapped.",
			}),
			DoneNo,
		},
		{
			"status-ping with done words",
			Format(&Message{
				Kind: KindStatusPing, Target: "T766.4", Status: ProgressInProgress,
				Payload: "Reading done; implementing next.",
			}),
			DoneNo,
		},
		{
			"malformed finish-report",
			"```jevons\njevons: kind finish-report\njevons: target T766.4\n```\n\nDone.",
			DoneUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyDone(tc.in); got != tc.want {
				t.Fatalf("ClassifyDone = %s, want %s", got, tc.want)
			}
			if got := IsDoneReport(tc.in); got != (tc.want == DoneYes) {
				t.Fatalf("IsDoneReport = %v, want %v", got, tc.want == DoneYes)
			}
		})
	}
}
