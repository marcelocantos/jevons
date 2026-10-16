// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package claudiapin_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiapin"
)

func TestCheckSymbolsAgainstReportsMissingTopAndField(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "internal", "demo")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package demo

import "github.com/marcelocantos/claudia"

func use() {
	_ = claudia.SandboxWritableRoots
	_ = claudia.AgentDef{Name: "x", FutureField: true}
	_ = claudia.Registry{}
}
`
	if err := os.WriteFile(filepath.Join(pkg, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := claudiapin.ExportIndex{
		PinVersion: "v0.28.0",
		Top: map[string]bool{
			"AgentDef": true,
			"Registry": true,
		},
		Fields: map[string]map[string]bool{
			"AgentDef": {"Name": true},
		},
	}
	r, err := claudiapin.CheckSymbolsAgainst(root, idx)
	if err != nil {
		t.Fatal(err)
	}
	if r.PinVersion != "v0.28.0" {
		t.Fatalf("PinVersion=%q", r.PinVersion)
	}
	if !claudiapin.SymbolsHardFail(r) {
		t.Fatal("expected hard fail on missing symbols")
	}
	text := claudiapin.FormatSymbols(r)
	for _, want := range []string{
		"🎯T608",
		"v0.28.0",
		"SandboxWritableRoots",
		"FutureField",
		"release gate",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("FormatSymbols missing %q:\n%s", want, text)
		}
	}
	// Registry is on the pin — must not be reported.
	for _, m := range r.Missing {
		if m.Name == "Registry" {
			t.Fatalf("Registry is on the pin but reported missing: %+v", m)
		}
	}
	var sawTop, sawField bool
	for _, m := range r.Missing {
		if m.Kind == "top" && m.Name == "SandboxWritableRoots" {
			sawTop = true
		}
		if m.Kind == "field" && m.Type == "AgentDef" && m.Name == "FutureField" {
			sawField = true
		}
	}
	if !sawTop || !sawField {
		t.Fatalf("missing refs incomplete: %+v", r.Missing)
	}
}

func TestCheckSymbolsAgainstCleanWhenPinHasAPI(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package pkg

import cli "github.com/marcelocantos/claudia"

func use() {
	_ = cli.AgentDef{Name: "x", SandboxWritableRoots: nil}
}
`
	if err := os.WriteFile(filepath.Join(pkg, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := claudiapin.ExportIndex{
		PinVersion: "v0.42.0",
		Top:        map[string]bool{"AgentDef": true},
		Fields: map[string]map[string]bool{
			"AgentDef": {"Name": true, "SandboxWritableRoots": true},
		},
	}
	r, err := claudiapin.CheckSymbolsAgainst(root, idx)
	if err != nil {
		t.Fatal(err)
	}
	if claudiapin.SymbolsHardFail(r) {
		t.Fatalf("unexpected missing: %s", claudiapin.FormatSymbols(r))
	}
	if !strings.Contains(claudiapin.FormatSymbols(r), "all on the published pin") {
		t.Fatalf("clean format: %s", claudiapin.FormatSymbols(r))
	}
}

func TestCheckSymbolsAgainstSkipsTestFiles(t *testing.T) {
	root := t.TempDir()
	src := `package x

import "github.com/marcelocantos/claudia"

func TestX(t *testing.T) { _ = claudia.NotOnPin }
`
	if err := os.WriteFile(filepath.Join(root, "x_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := claudiapin.ExportIndex{PinVersion: "v1.0.0", Top: map[string]bool{}}
	r, err := claudiapin.CheckSymbolsAgainst(root, idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Missing) != 0 {
		t.Fatalf("test files must not be scanned: %+v", r.Missing)
	}
}
