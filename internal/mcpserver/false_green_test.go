// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"io"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T386 acceptance 3: detection, not just doctrine. A finish report claiming
// green for a command whose own recorded output shows failure, timeout or
// panic is flagged on the way to the overseer — before it can be accepted and
// before a target retires on it.

func TestFalseGreenBannerFlagsGreenOverATimeoutPanic(t *testing.T) {
	report := "🎯T413 complete — make test-go is green.\n\n" +
		"    ok  \tgithub.com/marcelocantos/jevons/internal/thread\t0.4s\n" +
		"    panic: test timed out after 10m0s\n" +
		"    exit 0\n"

	banner := FalseGreenBanner(report)
	if banner == "" {
		t.Fatal("a green claimed over a timeout panic was not flagged")
	}
	if !strings.Contains(banner, string(gate.FlagOutputContradicts)) {
		t.Fatalf("banner does not name the contradiction: %q", banner)
	}
}

func TestFalseGreenBannerFlagsAPipelineStatus(t *testing.T) {
	report := "🎯T414 done, tests pass.\n\n" +
		"    $ go test ./... 2>&1 | tail -20\n" +
		"    --- FAIL: TestThreadResume (0.02s)\n" +
		"    $ echo \"exit=$?\"\n    exit=0\n"

	banner := FalseGreenBanner(report)
	if !strings.Contains(banner, string(gate.FlagPipelineMasked)) {
		t.Fatalf("pipeline-masked status not flagged: %q", banner)
	}
}

// The live evidence of 2026-08-10, in the shape a worker would report it.
func TestFalseGreenBannerFlagsTheZshArrayTrap(t *testing.T) {
	report := "🎯T415 web suite green.\n\n" +
		"    make test-web 2>&1 | tail -25; echo \"EXIT=${PIPESTATUS[0]}\"\n" +
		"    EXIT=\n"

	if banner := FalseGreenBanner(report); !strings.Contains(banner, string(gate.FlagShellArrayTrap)) {
		t.Fatalf("zsh PIPESTATUS trap not flagged: %q", banner)
	}
}

// The report the whole fleet writes on a good day must pass through clean.
// A banner on honest work trains the overseer to skim past banners, and the
// next real false green rides through behind that habit.
func TestFalseGreenBannerSilentOnHonestReports(t *testing.T) {
	for _, tc := range []struct{ name, report string }{
		{"unattested green", "🎯T412 done. make test-go passes, all packages ok. " +
			"Landed as e4c19aa."},
		{"honest red", "make test-go is RED: --- FAIL: TestX. Investigating, not done."},
		{"mid-turn progress", "Reading internal/thread now; go test not run yet."},
		{"accepted risk", "🎯T410 done with accepted risk: the daily path probe is " +
			"class-3 and residual: owner smoke required."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if banner := FalseGreenBanner(tc.report); banner != "" {
				t.Fatalf("honest report flagged: %q", banner)
			}
		})
	}
}

// A GATE line is worth something only because it is checkable. A worker who
// types one out rather than running the gate cites a record that does not
// exist, and the daemon says so — while a line produced by a real run passes
// clean. This is the property that makes the attestation more than a slogan.
func TestFalseGreenBannerChecksAttestationsAgainstTheStore(t *testing.T) {
	store, err := gate.OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	rec, err := gate.Run(&gate.RunArgs{
		Command: []string{"sh", "-c", "exit 0"},
		Name:    "make-test-go",
		Store:   store,
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	real := "🎯T412 done.\n\n    " + rec.Attestation() + "\n\nLanded as e4c19aa."
	if flags := gate.FlagFalseGreen(real, store.Lookup); len(flags) != 0 {
		t.Fatalf("a real attestation was flagged: %v", flags)
	}

	invented := "🎯T412 done.\n\n    GATE make-test-go exit=0 GREEN id=9f13c0a2 " +
		"out=6b1d9e4f2a01 dur=42.1s\n"
	flags := gate.FlagFalseGreen(invented, store.Lookup)
	if len(flags) == 0 || flags[0].Kind != gate.FlagAttestationUnknown {
		t.Fatalf("an invented attestation passed: %v", flags)
	}
}

// The banner is prepended, not merged into the report, so a long report
// cannot push the warning out of the delivered text. This asserts the
// composition the notify path performs.
func TestFalseGreenBannerLeadsTheDeliveredMessage(t *testing.T) {
	report := "Done, make test-go passes.\n\n    go test ./... | tail -5\n    exit=0\n"
	flags := FalseGreenFlags(report)
	if len(flags) == 0 {
		t.Fatal("fixture no longer trips the checker")
	}
	msg := gate.Banner(flags) + "\n\n" + "[Agent jv-t386-gate responded]\n" + report
	if !strings.HasPrefix(msg, gate.BannerHeading) {
		t.Fatalf("banner is not the first thing the overseer reads:\n%s", msg)
	}
	if !strings.Contains(msg, "[Agent jv-t386-gate responded]") {
		t.Fatal("the report itself was lost behind the banner")
	}
}
