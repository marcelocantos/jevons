// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestPathIsUnderStateDir(t *testing.T) {
	got := Path("/tmp/jevons-state")
	if got != filepath.Join("/tmp/jevons-state", FileName) {
		t.Fatalf("Path = %q", got)
	}
}

func TestNewRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	reg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Def("missing") != nil {
		t.Fatal("empty registry had a row")
	}
}

// New is the daemon's registry load path. Spool is a disposable event
// projection, not evidence that a saved Claudia generation may be replaced.
func TestRegistryReloadPreservesGenerationsWithoutSpool(t *testing.T) {
	dir := t.TempDir()
	spoolDir := filepath.Join(dir, "missing-spool")
	t.Setenv("JEVONS_SPOOL_DIR", spoolDir)
	path := Path(dir)
	reg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	providers := []claudia.Provider{
		"", claudia.ProviderClaude, claudia.ProviderCodex,
		claudia.ProviderGrok, claudia.ProviderCursor,
		"anthropic", "openai-codex", "xai-oauth",
	}
	defs := make([]claudia.AgentDef, 0, len(providers))
	for _, provider := range providers {
		name := "seat-" + string(provider)
		def := claudia.AgentDef{
			Name: name, Provider: provider, SessionID: "generation-" + name,
			Materialized: true, AutoStart: true,
		}
		if err := reg.Register(def); err != nil {
			t.Fatal(err)
		}
		defs = append(defs, def)
	}
	for _, stage := range []string{"initial reload", "reload after unrelated save"} {
		reg, err = New(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range defs {
			if got := reg.Def(want.Name); got == nil || !reflect.DeepEqual(*got, want) {
				t.Fatalf("%s changed %s: got %+v, want %+v", stage, want.Name, got, want)
			}
		}
		if err := reg.Register(claudia.AgentDef{
			Name: "unrelated", Provider: claudia.ProviderGrok, SessionID: "new-generation",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(spoolDir); !os.IsNotExist(err) {
		t.Fatalf("registry storage must not create a spool: %v", err)
	}
}

func TestJevonsdUsesSeatreg(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "cmd", "jevonsd", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	if !strings.Contains(src, "seatreg.New(") || !strings.Contains(src, "seatreg.Path(") {
		t.Fatal("jevonsd must construct the registry through seatreg")
	}
	if strings.Contains(src, "claudia.NewRegistry(") {
		t.Fatal("jevonsd still constructs claudia.NewRegistry directly")
	}
	// Every seat runs on the omp agent core; the upgrade handoff carries
	// identity only and must not resurrect CLI connect-mode.
	if strings.Contains(src, "GrokConnect = true") {
		t.Fatal("jevonsd restores legacy grok connect-mode; seats are omp-only")
	}
}
