// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
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

// 🎯T100 thin: NCA resolution + cross-tree deny names escalate target.
func TestNearestCommonAncestor(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	// root -> a -> a1
	//      -> b -> b1
	for _, d := range []claudia.AgentDef{
		{Name: "root", SessionID: "1", WorkDir: dir},
		{Name: "a", SessionID: "2", WorkDir: dir, Parent: "root"},
		{Name: "a1", SessionID: "3", WorkDir: dir, Parent: "a"},
		{Name: "b", SessionID: "4", WorkDir: dir, Parent: "root"},
		{Name: "b1", SessionID: "5", WorkDir: dir, Parent: "b"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	if got := nearestCommonAncestor(reg, "a1", "b1"); got != "root" {
		t.Fatalf("NCA(a1,b1)=%q want root", got)
	}
	if got := nearestCommonAncestor(reg, "a1", "a"); got != "a" {
		t.Fatalf("NCA(a1,a)=%q want a (ancestor)", got)
	}
	if got := nearestCommonAncestor(reg, "a", "a1"); got != "a" {
		t.Fatalf("NCA(a,a1)=%q want a", got)
	}
}

func TestCanKillCrossTreeNamesNCA(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "root", SessionID: "1", WorkDir: dir},
		{Name: "a", SessionID: "2", WorkDir: dir, Parent: "root"},
		{Name: "b", SessionID: "3", WorkDir: dir, Parent: "root"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	err = canKill(reg, "a", "b", func(string) bool { return false })
	if err == nil {
		t.Fatal("expected cross-tree deny")
	}
	msg := err.Error()
	if !strings.Contains(msg, "nearest common ancestor") || !strings.Contains(msg, "root") {
		t.Fatalf("deny should name NCA root, got %q", msg)
	}
	if !strings.Contains(msg, "justification") {
		t.Fatalf("deny should require justification, got %q", msg)
	}
	// Target must still be registered (no direct kill).
	if reg.Def("b") == nil {
		t.Fatal("target removed on deny")
	}
}
