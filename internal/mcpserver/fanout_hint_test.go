// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestLooksLikePOOrBoss(t *testing.T) {
	for _, name := range []string{"jevons-po", "tern-po", "sqlpipe_po", "release-boss", "po"} {
		if !looksLikePOOrBoss(name) {
			t.Errorf("%q should look like PO/boss", name)
		}
	}
	for _, name := range []string{"jevons", "jv-t111", "worker-a", ""} {
		if looksLikePOOrBoss(name) {
			t.Errorf("%q should not look like PO/boss", name)
		}
	}
}

// 🎯T111.4: zero-children PO is a detectable failure surface.
func TestFormatFanOutHintsZeroChildren(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "1", Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "2", Provider: "grok", Parent: "jevons"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	got := FormatFanOutHints(reg, "jevons")
	if !strings.Contains(got, "jevons-po") || !strings.Contains(got, "zero children") {
		t.Fatalf("want zero-children hint for po, got %q", got)
	}
	if !strings.Contains(got, "T111.4") {
		t.Fatalf("want target marker: %q", got)
	}
}

func TestFormatFanOutHintsWithChildrenClear(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "1", Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "2", Provider: "grok", Parent: "jevons"},
		{Name: "jv-t111", WorkDir: dir, SessionID: "3", Provider: "grok", Parent: "jevons-po"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	got := FormatFanOutHints(reg, "jevons")
	if got != "" {
		t.Fatalf("want no hint when children exist, got %q", got)
	}
}
