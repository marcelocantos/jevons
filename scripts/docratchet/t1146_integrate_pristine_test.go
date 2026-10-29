// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestT1146IntegrateRequiresPristineGate is the doctrine ratchet: landing
// must accept only bin/gate -clean measurements and refuse marcelocantos
// replaces committed in go.mod.
func TestT1146IntegrateRequiresPristineGate(t *testing.T) {
	pre := readRepo(t, "internal/worktree/t1107_gate_precondition.go")
	for _, needle := range []string{
		"MeasuredAsPristineCheckout",
		"ErrNonPristineGate",
		"refuseMarcelocantosReplaceInMod",
	} {
		if !strings.Contains(pre, needle) {
			t.Fatalf("t1107_gate_precondition.go missing 🎯T1146 seam %q", needle)
		}
	}
	mod := readRepo(t, "internal/worktree/gomod_replace.go")
	if !strings.Contains(mod, "ErrMarcelocantosReplace") {
		t.Fatal("gomod_replace.go missing ErrMarcelocantosReplace (🎯T1146)")
	}
	post := readRepo(t, "internal/worktree/t1146_post_merge_gate.go")
	for _, needle := range []string{
		"ErrPostMergeGate",
		"verifyMergeResultBuild",
	} {
		if !strings.Contains(post, needle) {
			t.Fatalf("t1146_post_merge_gate.go missing 🎯T1146 post-merge seam %q", needle)
		}
	}
	// Verify before land (2026-10-28): Integrate gates the merge commit and
	// only then moves the branch with the fast-forward. Landing first and
	// rolling back on RED left a window where a rolled-back merge read as
	// landed (aed4bd5f, bab71bb1).
	fset := token.NewFileSet()
	integrate, err := parser.ParseFile(fset, "integrate.go", readRepo(t, "internal/worktree/integrate.go"), 0)
	if err != nil {
		t.Fatal(err)
	}
	verifyAt, ffAt := token.NoPos, token.NoPos
	for _, decl := range integrate.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Integrate" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "verifyMergeResultBuild" && verifyAt == token.NoPos {
				verifyAt = call.Pos()
			}
			for _, arg := range call.Args {
				if lit, ok := arg.(*ast.BasicLit); ok && lit.Value == `"--ff-only"` && ffAt == token.NoPos {
					ffAt = call.Pos()
				}
			}
			return true
		})
	}
	if verifyAt == token.NoPos || ffAt == token.NoPos || verifyAt > ffAt {
		t.Fatalf("Integrate must call verifyMergeResultBuild before the --ff-only landing: verify=%v ff=%v", fset.Position(verifyAt), fset.Position(ffAt))
	}
	main := readRepo(t, "cmd/integrate/main.go")
	if !strings.Contains(main, "T1146") {
		t.Fatal("cmd/integrate/main.go must document 🎯T1146")
	}
}
