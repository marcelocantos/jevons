// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestPurposeCoordinator(t *testing.T) {
	for _, purpose := range []string{"product-owner", "Product-Owner", "boss", "Boss"} {
		if !purposeCoordinator(purpose) {
			t.Errorf("%q should be a coordinator purpose", purpose)
		}
	}
	for _, purpose := range []string{"", "work", "aside", "overseer", "jevons-po"} {
		if purposeCoordinator(purpose) {
			t.Errorf("%q should not be a coordinator purpose", purpose)
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
		{Name: "jevons-po", WorkDir: dir, SessionID: "2", Provider: "grok", Parent: "jevons", Purpose: purposeProductOwner},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	got := FormatFanOutHints(reg, "jevons", nil)
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
		{Name: "jevons-po", WorkDir: dir, SessionID: "2", Provider: "grok", Parent: "jevons", Purpose: purposeProductOwner},
		{Name: "jv-t111", WorkDir: dir, SessionID: "3", Provider: "grok", Parent: "jevons-po"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	got := FormatFanOutHints(reg, "jevons", nil)
	if got != "" {
		t.Fatalf("want no hint when children exist, got %q", got)
	}
}
