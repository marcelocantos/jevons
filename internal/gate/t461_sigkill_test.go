// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"io"
	"strings"
	"testing"
)

// 🎯T461 — a gate the host killed decided nothing. SIGKILL is its own
// verdict, citable neither as a pass nor as a failure.
//
// The live incident: with host swap exhausted, gate runs produced only
// "[killed]" and sessions died at exit 137. Those silently read as runs that
// happened. T386/T396 built GREEN/SUSPECT so a status could not be fabricated
// or lost in a pipeline; a host-killed run is a third thing neither covers.

// Oracle: SIGKILL a real gate subprocess mid-run and assert the recorded
// verdict is KILLED (acceptance 4).
func TestT461SigkillMidRunIsKilledNotRed(t *testing.T) {
	// kill -9 $$ shoots the shell gate started, mid-script — the same wait
	// status a host OOM / SIGKILL leaves. The echo after the kill must not run.
	rec, store := runIn(t, "sh", "-c", "echo starting; kill -9 $$; echo never-reached")

	if rec.Verdict != VerdictKilled {
		t.Fatalf("verdict = %s, want %s (status=%s note=%q anomalies=%v)",
			rec.Verdict, VerdictKilled, rec.Status(), rec.StatusNote, rec.Anomalies)
	}
	if rec.Verdict.IsGreen() {
		t.Fatal("a SIGKILL'd run was reportable as green")
	}
	if !strings.Contains(rec.Attestation(), "KILLED") {
		t.Fatalf("attestation does not name termination-by-signal: %s", rec.Attestation())
	}
	if !strings.Contains(rec.Summary(), "signal") && !strings.Contains(rec.StatusNote, "signal") {
		t.Fatalf("summary/note does not say the process was signalled:\n%s", rec.Summary())
	}
	if strings.Contains(rec.OutputTail, "never-reached") {
		t.Fatalf("kill did not land mid-run; output still has post-kill text: %q", rec.OutputTail)
	}

	got, ok, err := store.Load(rec.ID)
	if err != nil || !ok {
		t.Fatalf("Load(%s) = ok %v, err %v", rec.ID, ok, err)
	}
	if got.Verdict != VerdictKilled {
		t.Fatalf("stored verdict = %s, want %s", got.Verdict, VerdictKilled)
	}
}

// exit 137 is the shell / OOM convention for SIGKILL, even when WaitStatus
// reports a normal exit rather than Signaled.
func TestT461Exit137IsKilled(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c", "exit 137")

	if rec.Status() != "137" {
		t.Fatalf("status = %s, want 137", rec.Status())
	}
	if rec.Verdict != VerdictKilled {
		t.Fatalf("verdict = %s, want %s", rec.Verdict, VerdictKilled)
	}
	if !strings.Contains(rec.Attestation(), "exit=137 KILLED") {
		t.Fatalf("attestation = %q, want exit=137 KILLED", rec.Attestation())
	}
}

// Output that is only "[killed]" — the harness notice observed under host
// pressure — is a host kill even when the wait status looks like a pass.
func TestT461OutputOnlyKilledMarkerIsKilled(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c", "printf '%s\\n' '[killed]'; exit 0")

	if rec.Verdict != VerdictKilled {
		t.Fatalf("verdict = %s, want %s (a [killed]-only log was treated as a real result)",
			rec.Verdict, VerdictKilled)
	}
	if rec.Verdict.IsGreen() {
		t.Fatal("[killed]-only output was reportable as green")
	}
}

// Controls (acceptance 4 / 5): a genuine exit 1 is still FAIL/RED, and a
// genuine exit 0 is still GREEN. The over-broadness mutant — map every
// nonzero to KILLED — dies on the exit-1 control.
func TestT461ControlsExit1IsRedExit0IsGreen(t *testing.T) {
	red, _ := runIn(t, "sh", "-c", "echo boom >&2; exit 1")
	if red.Verdict != VerdictRed || red.Status() != "1" {
		t.Fatalf("exit 1 → %s exit=%s, want RED exit=1 (over-broad every-nonzero→killed fails here)",
			red.Verdict, red.Status())
	}

	green, _ := runIn(t, "sh", "-c", "echo ok; exit 0")
	if green.Verdict != VerdictGreen || green.Status() != "0" {
		t.Fatalf("exit 0 → %s exit=%s, want GREEN exit=0", green.Verdict, green.Status())
	}
}

// HostKill is the pure classifier the over-broadness mutation must not widen.
func TestT461HostKillClassifier(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		signaled, known         bool
		status                  int
		out                     string
		want                    bool
	}{
		{"signalled", true, false, 0, "", true},
		{"exit 137", false, true, 137, "cargo test …", true},
		{"only [killed]", false, true, 0, "[killed]\n", true},
		{"exit 1 control", false, true, 1, "FAIL", false},
		{"exit 0 control", false, true, 0, "ok", false},
		{"exit 2 not SIGKILL", false, true, 2, "", false},
		{"mentions killed in prose", false, true, 1, "the process was killed later", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HostKill(tc.signaled, tc.known, tc.status, tc.out); got != tc.want {
				t.Fatalf("HostKill(...) = %v, want %v", got, tc.want)
			}
		})
	}
}

// gate check refuses a killed run cited as passing evidence (acceptance 2).
func TestT461CheckRefusesKilledAsPass(t *testing.T) {
	rec, _ := runIn(t, "sh", "-c", "exit 137")
	report := "🎯T461 done — make test-go is green.\n\n    " + rec.Attestation() + "\n"

	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationKilled) {
		t.Fatalf("flags = %v, want %s for a killed run cited as a pass",
			kinds(flags), FlagAttestationKilled)
	}
	banner := Banner(flags)
	if !strings.Contains(banner, "FALSE-GREEN") {
		t.Fatalf("banner missing FALSE-GREEN heading:\n%s", banner)
	}
	if !strings.Contains(banner, "decided nothing") && !strings.Contains(banner, "KILLED") {
		t.Fatalf("banner does not name the killed refusal:\n%s", banner)
	}
}

// gate check refuses a killed run cited as FAILING / falsification evidence
// too (acceptance 3) — the T443 red-as-proof sibling, and the clause a lazy
// fix will skip.
func TestT461CheckRefusesKilledAsFailingEvidence(t *testing.T) {
	rec, err := Run(&RunArgs{
		Command: []string{"sh", "-c", "kill -9 $$"},
		Name:    "t461-prefix-red",
		Store:   mustStore(t),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Verdict != VerdictKilled {
		t.Fatalf("fixture verdict = %s, want KILLED", rec.Verdict)
	}

	// Same framing shape that exempts a genuine RED under 🎯T443. No blank
	// line: citationWindow stops at blanks, so the framing has to sit next
	// to the attestation the way workers actually write it.
	report := "with the wiring removed the oracles fail on their own assertions:\n    " +
		rec.Attestation() + "\n"

	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationKilled) {
		t.Fatalf("flags = %v, want %s — a killed run cited as falsification must still be refused",
			kinds(flags), FlagAttestationKilled)
	}
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("killed citation should use %s, not the generic not-green flag: %v",
			FlagAttestationKilled, kinds(flags))
	}
}

// A genuine RED framed as falsification still gets the T443 exemption — the
// killed refusal must not swallow that path.
func TestT461FalsificationRedStillExempt(t *testing.T) {
	rec, err := Run(&RunArgs{
		Command: []string{"sh", "-c", "echo FAIL; exit 1"},
		Name:    "t461-real-red",
		Store:   mustStore(t),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Verdict != VerdictRed {
		t.Fatalf("control verdict = %s, want RED", rec.Verdict)
	}

	report := "with the wiring removed the oracles fail on their own assertions:\n    " +
		rec.Attestation() + "\n"

	flags := FlagFalseGreen(report, nil)
	if hasKind(flags, FlagAttestationKilled) {
		t.Fatalf("a real red was classified as killed: %v", kinds(flags))
	}
	if hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("T443 exemption lost for a genuine falsification red: %v", kinds(flags))
	}
}

func mustStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return store
}
