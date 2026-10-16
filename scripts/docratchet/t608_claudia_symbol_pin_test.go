// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T608: landing jevons code against an unreleased claudia symbol must
// either be impossible, or the failure must name the missing claudia
// release — never a silent ordinary undefined: red inside T360.
//
// bin/claudiapin (and this oracle) fail when jevons source references a
// claudia symbol absent from the published pin. When the pin catches up,
// the check goes green with no further edits.
package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiapin"
)

// TestT608JevonsSymbolsAreOnPublishedPin is the load-bearing 🎯T608
// ratchet: every claudia.X selector and every keyed field of a
// claudia.T{…} composite in product source must exist on the go.mod pin
// under GOWORK=off. A miss names the pin version as a release gate.
func TestT608JevonsSymbolsAreOnPublishedPin(t *testing.T) {
	root := repoRoot(t)

	r, err := claudiapin.CheckSymbols(root)
	if err != nil {
		t.Fatal(err)
	}
	if claudiapin.SymbolsHardFail(r) {
		t.Fatalf("%s", claudiapin.FormatSymbols(r))
	}
	text := claudiapin.FormatSymbols(r)
	if !strings.Contains(text, "🎯T608") {
		t.Fatalf("clean report must still name T608:\n%s", text)
	}
	if r.PinVersion == "" {
		t.Fatal("expected pin version on symbol report")
	}
	// Sanity: pin version matches go.mod (GOWORK does not stand in).
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	want := claudiapin.RequireVersion(string(mod))
	if r.PinVersion != want {
		t.Fatalf("symbol report pin %s != go.mod %s", r.PinVersion, want)
	}
}

// TestT608ClaudiapinCLIWiresSymbolCheck keeps the CLI on the same path as
// the suite oracle: after a T448 commit check it must also print the
// T608 symbol block (and exit non-zero when symbols are missing — covered
// hermetically by internal/claudiapin tests).
func TestT608ClaudiapinCLIWiresSymbolCheck(t *testing.T) {
	root := repoRoot(t)
	main := readRepo(t, "cmd/claudiapin/main.go")
	for _, want := range []string{"CheckSymbols", "FormatSymbols", "SymbolsHardFail", "T608"} {
		if !strings.Contains(main, want) {
			t.Fatalf("cmd/claudiapin/main.go missing %q — symbol check not wired into bin/claudiapin", want)
		}
	}
	// Live CLI on this tree: must exit 0 and name T608 when the pin is
	// current (sibling-ahead alone is not a hard fail).
	bin := filepath.Join(t.TempDir(), "claudiapin")
	build := exec.Command("go", "build", "-o", bin, "./cmd/claudiapin")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build claudiapin: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, root)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		// A real pin gap is a legitimate red — still require the message
		// to name the release gate rather than a bare exit.
		if !strings.Contains(text, "🎯T608") && !strings.Contains(text, "T608") {
			t.Fatalf("claudiapin failed without T608 naming (%v):\n%s", err, text)
		}
		if !strings.Contains(text, "CLAUDIA PIN GAP") && !strings.Contains(text, "missing required") {
			t.Fatalf("claudiapin hard-fail must declare the gate:\n%s", text)
		}
		return
	}
	if !strings.Contains(text, "🎯T608") {
		t.Fatalf("claudiapin stdout must include T608 symbol block:\n%s", text)
	}
}
