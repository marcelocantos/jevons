// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import "testing"

// A `go test -json` run whose test prints a child suite's -json stream (the
// integrate rollback tests do) is not a failure: four passing post-merge
// gates were graded SUSPECT on that nested `--- FAIL` (2026-10-28).
func TestScanOutputIgnoresNestedChildJSONFailure(t *testing.T) {
	out := `{"Time":"2026-10-28T06:29:20.707799+11:00","Action":"output","Package":"x/internal/worktree","Test":"TestIntegrateRollsBack","Output":"{\"Time\":\"2026-10-28T06:29:20.7+11:00\",\"Action\":\"output\",\"Package\":\"example.com/lib\",\"Test\":\"TestValueIsOne\",\"Output\":\"--- FAIL: TestValueIsOne (0.00s)\\n\"}\n"}
{"Time":"2026-10-28T06:29:21Z","Action":"pass","Package":"x/internal/worktree","Test":"TestIntegrateRollsBack","Elapsed":1}`
	if got := ScanOutput(out); len(got) != 0 {
		t.Fatalf("nested child failure graded as an anomaly: %+v", got)
	}
}

// A top-level -json event whose own Output is a failure still counts.
func TestScanOutputStillSeesTopLevelJSONFailure(t *testing.T) {
	out := `{"Time":"2026-10-28T06:29:20Z","Action":"output","Package":"x/lib","Test":"TestReal","Output":"--- FAIL: TestReal (0.00s)\n"}`
	got := ScanOutput(out)
	if len(got) != 1 || got[0].Marker != "--- FAIL" {
		t.Fatalf("top-level -json failure missed: %+v", got)
	}
}
