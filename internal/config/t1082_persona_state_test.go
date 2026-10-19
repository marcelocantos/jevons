// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"
	"testing"
)

// 🎯T1082: an isolate overseer, never told where its own state lived and
// told that development state is ~/.jevons, ran
// `sqlite3 ~/.jevons/jevons.db "SELECT name FROM sqlite_master …"` from
// inside the journey suite (gate 252cf6ac, overseer transcript row 40),
// opening the development database read-write. A daemon on another state
// directory now names its own and forbids the development one.
func TestT1082IsolatePersonaNamesItsOwnStateDir(t *testing.T) {
	cfg := Default()
	cfg.StateDir = "/var/folders/xx/T/jevons-journey-1234"
	out, err := cfg.Persona()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"This daemon's state (🎯T1082)",
		"Its state directory is\n`/var/folders/xx/T/jevons-journey-1234`",
		"Never read, list, open,\nquery or write anything under `~/.jevons`",
		"means `/var/folders/xx/T/jevons-journey-1234/...` for you.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("isolate persona is missing %q", want)
		}
	}
	dev, err := Default().Persona()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dev, "This daemon's state (🎯T1082)") {
		t.Fatal("the development daemon's persona must not tell it to avoid its own state")
	}
	if !strings.Contains(dev, "## Your Role") || strings.Contains(dev, "{{") {
		t.Fatal("development persona did not render cleanly")
	}
}
