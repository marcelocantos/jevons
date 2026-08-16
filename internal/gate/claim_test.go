// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"io"
	"strings"
	"testing"
)

// The three fixture finish reports 🎯T386 acceptance 4 names. They are
// written in the register real workers use, because a classifier tuned on
// tidy synthetic prose is tuned on the wrong distribution.
const (
	// A genuine green: the gate ran, the status is its own, nothing in the
	// report argues with it. This one must stay unflagged — see the
	// over-broadness test.
	reportTrueGreen = `🎯T412 done.

Ran the suite through the gate:

    bin/gate -- make test-go
    GATE make-test-go exit=0 GREEN id=9f13c0a2 out=6b1d9e4f2a01 dur=42.1s

Landed as e4c19aa. All packages pass; nothing skipped.`

	// A green claimed over output that shows the suite died. This is the
	// claudia-po case: the number said zero, the log said timeout panic.
	reportGreenOverPanic = `🎯T413 complete — make test is green.

    $ make test-go
    ok  	github.com/marcelocantos/jevons/internal/thread	0.4s
    panic: test timed out after 10m0s
    ...
    exit 0

Calling that a pass and moving on.`

	// A green claimed from a pipeline's status. tail always succeeds, so the
	// cited zero is tail's, and the FAIL lines above it are the suite's.
	reportPipedExitCode = `🎯T414 finished, tests pass.

    $ go test ./... 2>&1 | tail -20
    --- FAIL: TestThreadResume (0.02s)
    FAIL	github.com/marcelocantos/jevons/internal/thread	0.31s
    $ echo "exit=$?"
    exit=0

Green, so I'm reporting done.`

	// The live evidence from 2026-08-10, reproduced verbatim in shape: zsh
	// has no PIPESTATUS, so the interpolation was empty and the status was
	// never read at all.
	reportZshPipestatus = "🎯T415 web suite green.\n\n" +
		"    make test-web 2>&1 | tail -25; echo \"EXIT=${PIPESTATUS[0]}\"\n" +
		"    EXIT=\n\n" +
		"No failures printed, so: passed."
)

func kinds(flags []Flag) []FlagKind {
	out := make([]FlagKind, 0, len(flags))
	for _, f := range flags {
		out = append(out, f.Kind)
	}
	return out
}

func hasKind(flags []Flag, want FlagKind) bool {
	for _, f := range flags {
		if f.Kind == want {
			return true
		}
	}
	return false
}

// 🎯T386 acceptance 4, the flagging half.
func TestT386FalseGreensAreFlagged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report string
		want   FlagKind
	}{
		{"green over a timeout panic", reportGreenOverPanic, FlagOutputContradicts},
		{"green from a pipeline's status", reportPipedExitCode, FlagPipelineMasked},
		{"zsh PIPESTATUS that never expanded", reportZshPipestatus, FlagShellArrayTrap},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := FlagFalseGreen(tc.report, nil)
			if !hasKind(flags, tc.want) {
				t.Fatalf("flags = %v, want one of kind %s", kinds(flags), tc.want)
			}
			if Banner(flags) == "" {
				t.Fatal("flagged report produced no banner")
			}
		})
	}
}

// 🎯T386 acceptance 4, the other half — and the mutation that matters most.
// A checker that flags honest reports is worse than no checker: it teaches
// the reader to skim past the banner, and the next real false green rides
// through behind that habit.
func TestT386TrueGreenIsNotFlagged(t *testing.T) {
	if flags := FlagFalseGreen(reportTrueGreen, nil); len(flags) != 0 {
		t.Fatalf("a genuine green was flagged: %v", flags)
	}
	if Banner(nil) != "" {
		t.Fatal("empty flag list produced a banner")
	}
}

// Prose about a panic is not a panic, and a report that describes a piped
// command without attesting to its status is describing, not claiming.
func TestFlagFalseGreenIsNarrow(t *testing.T) {
	for _, tc := range []struct{ name, report string }{
		{"prose about a fixed panic", "🎯T400 done: fixed the panic in thread resume. " +
			"make test-go passes, commit e4c19aa."},
		{"a piped command with no status claim", "For reading long output I use " +
			"`go test ./... | tail -20`; that is not how I gate."},
		{"no completion claim at all", "Still working. go test is running now."},
		{"failure honestly reported", "make test-go is RED: --- FAIL: TestX. Investigating."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if flags := FlagFalseGreen(tc.report, nil); len(flags) != 0 {
				t.Fatalf("flagged an honest report: %v", flags)
			}
		})
	}
}

// A report may not cite a non-green attestation as if it settled anything.
func TestCitedAttestationMustBeGreen(t *testing.T) {
	report := "Done — GATE make-test-go exit=1 RED id=deadbeef out=aaaaaaaaaaaa dur=3s, " +
		"but the failure is unrelated so I'm calling it passed."
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s", kinds(flags), FlagAttestationNotGreen)
	}
}

// An attestation is only worth anything if it can be checked. A cited id with
// no record behind it was not produced by a run on this machine.
func TestCitedAttestationCheckedAgainstStore(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	real, err := Run(&RunArgs{
		Command: []string{"sh", "-c", "exit 1"},
		Name:    "make-test-go",
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	t.Run("id with no record", func(t *testing.T) {
		report := "Done. GATE make-test-go exit=0 GREEN id=cafebabe out=000000000000 dur=1s"
		flags := FlagFalseGreen(report, store.Lookup)
		if !hasKind(flags, FlagAttestationUnknown) {
			t.Fatalf("flags = %v, want %s", kinds(flags), FlagAttestationUnknown)
		}
	})

	t.Run("id whose record disagrees", func(t *testing.T) {
		report := "Done. GATE make-test-go exit=0 GREEN id=" + real.ID + " out=000000000000 dur=1s"
		flags := FlagFalseGreen(report, store.Lookup)
		if !hasKind(flags, FlagAttestationContradicted) {
			t.Fatalf("flags = %v, want %s", kinds(flags), FlagAttestationContradicted)
		}
	})

	t.Run("the real attestation checks out", func(t *testing.T) {
		// Run outside a git work tree so provenance is unknown, not
		// dirty: this case is "the cited record exists and agrees",
		// not 🎯T397's dirty-tree-cited-for-a-commit flag.
		green, err := Run(&RunArgs{
			Command: []string{"sh", "-c", "exit 0"},
			Name:    "make-test-go",
			Dir:     t.TempDir(),
			Store:   store,
			Stdout:  io.Discard,
			Stderr:  io.Discard,
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		report := "🎯T412 done, commit e4c19aa.\n\n    " + green.Attestation() + "\n"
		if flags := FlagFalseGreen(report, store.Lookup); len(flags) != 0 {
			t.Fatalf("a real green attestation was flagged: %v", flags)
		}
	})
}

func TestParseAttestationsRoundTrip(t *testing.T) {
	rec := &Record{
		ID: "9f13c0a2", Name: "make-test-go", ExitStatus: 0, StatusKnown: true,
		Verdict: VerdictGreen, OutputSHA256: strings.Repeat("a", 64),
	}
	cited := ParseAttestations("prefix\n  " + rec.Attestation() + "\nsuffix")
	if len(cited) != 1 {
		t.Fatalf("parsed %d attestations, want 1", len(cited))
	}
	got := cited[0]
	if got.ID != rec.ID || got.Name != rec.Name || got.Verdict != VerdictGreen || !got.StatusIsZero() {
		t.Fatalf("round trip lost fields: %+v", got)
	}
}

// "unknown" must not be readable as a zero anywhere on the reporting path.
func TestUnknownStatusIsNotZero(t *testing.T) {
	c := CitedAttestation{Status: UnknownStatus, Verdict: VerdictUnknown}
	if c.StatusIsZero() {
		t.Fatal("unknown read as zero")
	}
	report := "Done — GATE make-test exit=unknown UNKNOWN id=abc12345 out=000000000000 dur=1s"
	if flags := FlagFalseGreen(report, nil); !hasKind(flags, FlagAttestationNotGreen) {
		t.Fatalf("flags = %v, want %s", kinds(flags), FlagAttestationNotGreen)
	}
}

func TestBannerNamesTheFix(t *testing.T) {
	b := Banner(FlagFalseGreen(reportPipedExitCode, nil))
	if !strings.Contains(b, BannerHeading) {
		t.Fatalf("banner missing heading: %q", b)
	}
	if !strings.Contains(b, "bin/gate") {
		t.Fatalf("banner does not name the mechanism to use instead: %q", b)
	}
}
