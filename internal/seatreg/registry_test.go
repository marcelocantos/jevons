// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(src, "seatreg.RemintRegistry(") {
		t.Fatal("jevonsd must remint the fleet onto the sidecar")
	}
	merge := strings.LastIndex(src, "def.GrokConnect = true")
	remint := strings.LastIndex(src, "seatreg.RemintRegistry(")
	if merge < 0 || remint < merge {
		t.Fatal("sidecar remint must run after upgrade handoff restores grok connect")
	}
}
