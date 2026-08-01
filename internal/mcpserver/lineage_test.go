// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestIsAncestorAndDescendants(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "root", SessionID: "1", WorkDir: dir},
		{Name: "mid", SessionID: "2", WorkDir: dir, Parent: "root"},
		{Name: "leaf", SessionID: "3", WorkDir: dir, Parent: "mid"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	if !reg.IsAncestor("root", "leaf") {
		t.Fatal("root should be ancestor of leaf")
	}
	if !reg.IsAncestor("mid", "leaf") {
		t.Fatal("mid should be ancestor of leaf")
	}
	if reg.IsAncestor("leaf", "root") {
		t.Fatal("leaf is not ancestor of root")
	}
	if reg.IsAncestor("root", "root") {
		t.Fatal("not strict ancestor of self")
	}
	desc := reg.Descendants("root")
	if len(desc) != 2 {
		t.Fatalf("descendants = %v, want mid+leaf", desc)
	}
}
