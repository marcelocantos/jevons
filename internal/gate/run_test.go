// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"io"
	"strings"
	"testing"
)

// runIn runs a gate in a throwaway store and returns the record.
func runIn(t *testing.T, argv ...string) (*Record, *Store) {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	rec, err := Run(&RunArgs{
		Command: argv,
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run(%v): %v", argv, err)
	}
	return rec, store
}

// 🎯T396 acceptance 1: a command that exits non-zero surfaces non-zero.
func TestT396FailingCommandSurfacesNonZero(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c", "echo boom >&2; exit 1")

	if !rec.StatusKnown || rec.ExitStatus != 1 {
		t.Fatalf("exit status = %s (known=%v), want 1", rec.Status(), rec.StatusKnown)
	}
	if rec.Verdict != VerdictRed {
		t.Fatalf("verdict = %s, want %s", rec.Verdict, VerdictRed)
	}
	if strings.Contains(rec.Attestation(), "GREEN") {
		t.Fatalf("a failing run attested as green: %s", rec.Attestation())
	}
	if !strings.Contains(rec.Attestation(), "exit=1") {
		t.Fatalf("attestation does not carry the real status: %s", rec.Attestation())
	}
}

// The over-broadness mutation 🎯T396 acceptance 4 asks for: a wrapper that
// calls successful runs failures is as broken as one that calls failures
// successes, and gets switched off just as fast.
func TestT396SucceedingCommandSurfacesZeroAndGreen(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c", "echo ok; exit 0")

	if !rec.StatusKnown || rec.ExitStatus != 0 {
		t.Fatalf("exit status = %s, want 0", rec.Status())
	}
	if rec.Verdict != VerdictGreen {
		t.Fatalf("verdict = %s, want %s (anomalies %v)", rec.Verdict, VerdictGreen, rec.Anomalies)
	}
	if !strings.Contains(rec.Attestation(), "exit=0 GREEN") {
		t.Fatalf("attestation = %q, want a green", rec.Attestation())
	}
}

// A shell pipeline the worker writes CORRECTLY still surfaces non-zero: gate
// relays the command's own status and does not add a masking layer of its
// own (🎯T396 acceptance 2, pipeline case).
func TestT396CorrectPipelineStatusSurvivesTheGate(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c", "set -o pipefail; exit 7 | cat")

	if rec.Status() != "7" {
		t.Fatalf("exit status = %s, want 7 — the gate lost the pipeline's status", rec.Status())
	}
	if rec.Verdict != VerdictRed {
		t.Fatalf("verdict = %s, want %s", rec.Verdict, VerdictRed)
	}
}

// The claudia-po case: status says pass, output says timeout panic. This must
// never render as a green, because it is the exact shape that nearly retired
// a target on a suite that had died (🎯T386).
func TestT386ZeroExitWithPanicOutputIsSuspectNotGreen(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c",
		`echo "panic: test timed out after 10m0s"; exit 0`)

	if rec.Status() != "0" {
		t.Fatalf("exit status = %s, want 0 (the point is that the status lies)", rec.Status())
	}
	if rec.Verdict != VerdictSuspect {
		t.Fatalf("verdict = %s, want %s", rec.Verdict, VerdictSuspect)
	}
	if rec.Verdict.IsGreen() {
		t.Fatal("a suite that timed out was reportable as green")
	}
	if len(rec.Anomalies) == 0 {
		t.Fatal("no anomaly recorded for a timeout panic")
	}
}

// 🎯T396 acceptance 3: a status the gate cannot establish renders as
// "unknown", never as zero. An unrunnable command is the cheap way to reach
// that branch; a signal death reaches the same one.
func TestT396UnknownStatusNeverRendersAsZero(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	rec, err := Run(&RunArgs{
		Command: []string{"jevons-no-such-command-exists"},
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if rec == nil {
		t.Fatalf("Run returned no record: %v", err)
	}
	if rec.StatusKnown {
		t.Fatalf("status reported as known (%d) for a command that never ran", rec.ExitStatus)
	}
	if rec.Status() != UnknownStatus {
		t.Fatalf("status renders as %q, want %q", rec.Status(), UnknownStatus)
	}
	if rec.Verdict.IsGreen() {
		t.Fatal("an unknown status was reportable as green")
	}
	if !strings.Contains(rec.Attestation(), "exit=unknown") {
		t.Fatalf("attestation = %q, want exit=unknown", rec.Attestation())
	}
}

// The record is the in-band channel: after the run, the truth about the
// status is on disk and can be read by something that never saw the process.
// This is what makes a misreported background gate recoverable (🎯T396).
func TestT396RecordReadBackCarriesTheRealStatus(t *testing.T) {
	rec, store := runIn(t, "sh", "-c", "echo nope >&2; exit 5")

	got, ok, err := store.Load(rec.ID)
	if err != nil || !ok {
		t.Fatalf("Load(%s) = ok %v, err %v", rec.ID, ok, err)
	}
	if got.Status() != "5" || got.Verdict != VerdictRed {
		t.Fatalf("record says exit=%s %s, want exit=5 %s", got.Status(), got.Verdict, VerdictRed)
	}
	if !strings.Contains(got.OutputTail, "nope") {
		t.Fatalf("record lost the output: %q", got.OutputTail)
	}
}

// The captured log is kept alongside the record so a reader can check the
// digest the attestation cites.
func TestGateCapturesOutputAndDigest(t *testing.T) {
	rec, store := runIn(t, "sh", "-c", "echo hello; echo world >&2")

	if rec.OutputBytes == 0 || rec.OutputSHA256 == "" {
		t.Fatalf("no output recorded: %+v", rec)
	}
	if !strings.Contains(rec.OutputTail, "hello") || !strings.Contains(rec.OutputTail, "world") {
		t.Fatalf("tail lost a stream: %q", rec.OutputTail)
	}
	if got := store.LogPath(rec.ID); rec.OutputPath != got {
		t.Fatalf("OutputPath = %q, want %q", rec.OutputPath, got)
	}
	if !strings.Contains(rec.Attestation(), "out="+rec.OutputSHA256[:digestPrefix]) {
		t.Fatalf("attestation does not tie to the log: %s", rec.Attestation())
	}
}

// A run with no store still reports honestly — it just leaves no evidence.
func TestGateWithoutStoreStillReportsStatus(t *testing.T) {
	rec, err := Run(&RunArgs{
		Command: []string{"sh", "-c", "exit 2"},
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status() != "2" || rec.OutputPath != "" {
		t.Fatalf("rec = %s, path %q", rec.Attestation(), rec.OutputPath)
	}
}

func TestGateNameDerivation(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"make", "test-go"}, "make-test-go"},
		{[]string{"go", "test", "./..."}, "go-test"},
		{[]string{"node", "web/scripts/x_test.js"}, "node"},
	} {
		if got := gateName("", tc.argv); got != tc.want {
			t.Errorf("gateName(%v) = %q, want %q", tc.argv, got, tc.want)
		}
	}
	if got := gateName("web suite", []string{"make", "test-web"}); got != "web-suite" {
		t.Errorf("explicit name = %q, want web-suite", got)
	}
}
