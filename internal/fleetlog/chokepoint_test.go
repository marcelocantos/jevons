// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleetlog

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 🎯T435 clause 1: every removal of an agent from the registry is accounted
// for in the event log. The behavioural tests below show the chokepoint emits
// the event; this test shows nothing goes around it, which is the half that
// decays. The unlogged 2026-08-10 achieve-reap was one line of ordinary code
// in a file nobody was watching.

// registryReceiver matches an expression that names an agent registry —
// reg, registry, s.registry, f.reg, a.registry. A Remove on one of those is
// a fleet removal and belongs in this package.
var registryReceiver = regexp.MustCompile(`(?i)reg`)

// removalFinding is one registry Remove found outside the chokepoint.
type removalFinding struct {
	File     string
	Line     int
	Receiver string
}

func (f removalFinding) String() string {
	return fmt.Sprintf("%s:%d: %s.Remove(...)", f.File, f.Line, f.Receiver)
}

// scanRegistryRemovals reports every `<registry>.Remove(...)` call in file.
func scanRegistryRemovals(fset *token.FileSet, file *ast.File, path string) []removalFinding {
	var out []removalFinding
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Remove" {
			return true
		}
		recv := exprString(fset, sel.X)
		if !registryReceiver.MatchString(recv) {
			return true
		}
		out = append(out, removalFinding{
			File:     path,
			Line:     fset.Position(sel.Sel.Pos()).Line,
			Receiver: recv,
		})
		return true
	})
	return out
}

func exprString(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "<unprintable>"
	}
	return b.String()
}

// TestT435EveryRegistryRemovalIsAccounted fails when production code removes
// an agent from the registry without going through this package — the exact
// shape of the defect this target was filed for.
func TestT435EveryRegistryRemovalIsAccounted(t *testing.T) {
	root := repoRoot(t)
	var findings []removalFinding
	for _, dir := range []string{"internal", "cmd"} {
		base := filepath.Join(root, dir)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "testdata" || info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			// The chokepoint is the one place allowed to call the registry.
			if filepath.Dir(path) == filepath.Join(root, "internal", "fleetlog") {
				return nil
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return fmt.Errorf("parse %s: %w", path, perr)
			}
			rel, _ := filepath.Rel(root, path)
			findings = append(findings, scanRegistryRemovals(fset, f, rel)...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	if len(findings) > 0 {
		var lines []string
		for _, f := range findings {
			lines = append(lines, "  "+f.String())
		}
		t.Fatalf("🎯T435: %d registry removal(s) bypass the accounted chokepoint —\n%s\n"+
			"a removal that does not go through fleetlog.Account leaves a registry diff "+
			"with no event to explain it. Use Account.Remove / Account.RemoveSubtree "+
			"with a fleetlog.Reason.", len(findings), strings.Join(lines, "\n"))
	}
}

// TestT435ScannerDetectsAnUnaccountedRemoval is the control: it proves the
// ratchet above can fail. A scanner that finds nothing because it looks for
// nothing reports the same green as a clean tree.
func TestT435ScannerDetectsAnUnaccountedRemoval(t *testing.T) {
	const bypass = `package demo

import "github.com/marcelocantos/claudia"

func reap(reg *claudia.Registry, name string) error {
	return reg.Remove(name)
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "bypass.go", bypass, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	got := scanRegistryRemovals(fset, f, "bypass.go")
	if len(got) != 1 {
		t.Fatalf("scanner missed the unaccounted removal: got %d findings, want 1", len(got))
	}
	if got[0].Receiver != "reg" || got[0].Line != 6 {
		t.Fatalf("finding = %+v, want reg.Remove at line 6", got[0])
	}

	// And the accounted form is not flagged, so the ratchet does not push
	// callers away from the chokepoint it exists to enforce.
	const accounted = `package demo

import "github.com/marcelocantos/claudia"

func reap(acct *Account, reg *claudia.Registry, name string) error {
	_, err := acct.Remove(reg, name, Removal{Reason: ReasonReapDone})
	return err
}
`
	f2, err := parser.ParseFile(fset, "accounted.go", accounted, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if got := scanRegistryRemovals(fset, f2, "accounted.go"); len(got) != 0 {
		t.Fatalf("accounted removal flagged: %+v", got)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}
