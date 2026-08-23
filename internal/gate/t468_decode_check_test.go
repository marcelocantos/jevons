// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T468. `gate check <stored-report>.json` used to pass raw envelope bytes to
// FlagFalseGreen, so it scanned escaped `\n` and disagreed with the daemon
// (and with `gate check < decoded-text`) about the same report. The two forms
// must yield identical flags; plain-text reports stay unchanged.

func t468ReportText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "t443_t380_prefix_red_report.md"))
	if err != nil {
		t.Fatalf("read t380 fixture: %v", err)
	}
	text := string(b)
	if !strings.Contains(text, "GATE t380-prefix-red exit=1 RED id=7ff7f677") {
		t.Fatal("fixture missing the RED citation T443/T468 turn on")
	}
	return text
}

func t468EnvelopePath(t *testing.T, text string) string {
	t.Helper()
	state := t.TempDir()
	rec, err := agentreport.Save(state, "jv-t380-auto", text,
		time.Date(2026, 8, 14, 22, 43, 15, 0, time.UTC))
	if err != nil {
		t.Fatalf("Save envelope: %v", err)
	}
	return filepath.Join(state, agentreport.DirName, "jv-t380-auto", rec.ID+".json")
}

// Package-level agreement: decoding the envelope and checking the bare text
// produce the same flag set. This is the hermetic half of acceptance — no
// binary, no stdin.
func TestT468PathDecodeAndBareTextYieldIdenticalFlags(t *testing.T) {
	text := t468ReportText(t)
	path := t468EnvelopePath(t, text)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if agentreport.DecodeBody(raw) != text {
		t.Fatal("DecodeBody did not recover the saved report text")
	}

	fromEnvelope := gate.FlagFalseGreen(agentreport.DecodeBody(raw), nil)
	fromText := gate.FlagFalseGreen(text, nil)
	if got, want := kindsOf(fromEnvelope), kindsOf(fromText); got != want {
		t.Fatalf("envelope flags %v != text flags %v", got, want)
	}
	if len(fromText) != 0 {
		t.Fatalf("honest t380 report must clear; flags=%v\n%s",
			kindsOf(fromText), gate.Banner(fromText))
	}

	// Control: raw envelope bytes without DecodeBody disagree — that is the
	// pre-fix bug. If this ever stops failing, the fixture no longer
	// exercises the escaped-newline trap and the acceptance is hollow.
	rawFlags := gate.FlagFalseGreen(string(raw), nil)
	if len(rawFlags) == 0 {
		t.Fatal("raw envelope bytes cleared FlagFalseGreen; fixture no longer " +
			"reproduces the T468 scan bug (escaped framing destroyed)")
	}
}

// Shipped-binary agreement: `gate check <path>` and `gate check < text` both
// print `no false-green flags` and exit 0 when the cited GREEN ids exist in
// the store — the acceptance case measured against daily ~/.jevons/gates.
// Pre-fix, path form still banners from escaped JSON even with those
// records present.
func TestT468GateCheckPathAgreesWithDecodedStdin(t *testing.T) {
	storeDir := t.TempDir()
	store, err := gate.OpenStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the three GREEN citations in the t380 report so Lookup clears.
	for _, id := range []string{"31800369", "5ce9c558", "71f69c98"} {
		if err := store.Save(&gate.Record{
			ID: id, Name: "seeded", ExitStatus: 0, StatusKnown: true,
			Verdict: gate.VerdictGreen,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	text := t468ReportText(t)
	path := t468EnvelopePath(t, text)

	codePath, outPath := runGate(t, storeDir, "check", path)
	codeText, outText := runGateWithStdin(t, storeDir, text, "check")
	if codePath != 0 || codeText != 0 {
		t.Fatalf("want exit 0 on both forms; path=%d stdin=%d\npath:\n%s\nstdin:\n%s",
			codePath, codeText, outPath, outText)
	}
	if !strings.Contains(outPath, "no false-green flags") ||
		!strings.Contains(outText, "no false-green flags") {
		t.Fatalf("want quiet success on both forms\npath:\n%s\nstdin:\n%s", outPath, outText)
	}
	if strings.TrimSpace(outPath) != strings.TrimSpace(outText) {
		t.Fatalf("path and stdin verdicts differ\npath:\n%s\nstdin:\n%s", outPath, outText)
	}

	// Pre-fix control via the package API: raw envelope bytes still raise
	// flags the decoded text does not (nil lookup so store noise cannot mask
	// the framing bug).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rawFlags := gate.FlagFalseGreen(string(raw), nil)
	textFlags := gate.FlagFalseGreen(text, nil)
	if len(textFlags) != 0 {
		t.Fatalf("decoded text flagged under nil lookup: %v", kindsOf(textFlags))
	}
	if len(rawFlags) == 0 {
		t.Fatal("raw envelope cleared under nil lookup; fixture no longer reproduces T468")
	}
}

// Plain-text path form is unchanged (🎯T453 still holds after the decoder).
func TestT468PlainTextReportPathUnchanged(t *testing.T) {
	store := t.TempDir()
	honest := filepath.Join(t.TempDir(), "honest.md")
	if err := os.WriteFile(honest, []byte("Still working; no GATE line yet.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runGate(t, store, "check", honest); code != 0 {
		t.Fatalf("plain-text honest report flagged after T468: exit %d\n%s", code, out)
	}

	falseGreen := filepath.Join(t.TempDir(), "piped.md")
	body := "tests pass.\n\n    $ go test ./... 2>&1 | tail -20\n    --- FAIL: TestX (0.01s)\n    exit=0\n"
	if err := os.WriteFile(falseGreen, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := runGate(t, store, "check", falseGreen)
	if code != 4 {
		t.Fatalf("plain-text false green exited %d, want 4\n%s", code, out)
	}
	if !strings.Contains(out, string(gate.FlagPipelineMasked)) {
		t.Fatalf("plain-text false green lost its pipeline flag:\n%s", out)
	}
}

func runGateWithStdin(t *testing.T, storeDir, stdin string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(gateBinary(t), args...)
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+storeDir)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, isExit := errAsExit(err); !isExit {
			t.Fatalf("running gate %v: %v (%s)", args, err, out)
		}
	}
	return cmd.ProcessState.ExitCode(), string(out)
}

func kindsOf(flags []gate.Flag) string {
	if len(flags) == 0 {
		return ""
	}
	parts := make([]string, len(flags))
	for i, f := range flags {
		parts[i] = string(f.Kind)
	}
	return strings.Join(parts, ",")
}
