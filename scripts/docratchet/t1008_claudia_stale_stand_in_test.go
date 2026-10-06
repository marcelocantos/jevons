// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// claimsClaudiaLacksAPI matches the doc-comment wording jevons uses when a
// stand-in exists only because the published claudia pin does not export
// the equivalent API. 🎯T1008: a comment making this claim about the
// symbol it documents must stay true as the pin advances — the pin has
// drifted ahead of such a claim at least twice already (destAuthor /
// claudia.DecisionAuthor, codex_receipt.go's exclusiveCodexHomeDir
// cross-reference).
var claimsClaudiaLacksAPI = regexp.MustCompile(`(?i)claudia[^.]{0,80}does not export|published (claudia )?(module|pin)[^.]{0,40}does not export`)

// TestT1008StandInCommentsMatchThePinnedClaudiaExports is the load-bearing
// 🎯T1008 ratchet: for every exported Go declaration whose doc comment
// claims "claudia ... does not export" itself, the declared symbol's own
// name must NOT be present among the pinned claudia module's exported
// top-level identifiers. A comment that goes stale the moment the pin
// catches up — rather than silently rotting — fails this test.
func TestT1008StandInCommentsMatchThePinnedClaudiaExports(t *testing.T) {
	root := repoRoot(t)
	exported := pinnedClaudiaExports(t, root)

	fset := token.NewFileSet()
	var stale []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == "node_modules" || name == ".git" || strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Only Jevons's own source; the claudia module cache is read
		// separately as the oracle, not scanned for its own claims.
		if strings.Contains(path, filepath.Join("pkg", "mod")) {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
		if perr != nil {
			// Non-buildable scratch files are not this ratchet's concern.
			return nil
		}
		for _, decl := range f.Decls {
			doc, name := declDoc(decl)
			if doc == nil || name == "" {
				continue
			}
			if !claimsClaudiaLacksAPI.MatchString(doc.Text()) {
				continue
			}
			if exported[name] {
				rel, _ := filepath.Rel(root, path)
				stale = append(stale, rel+": "+name+" is claimed as a claudia stand-in, "+
					"but the pinned claudia module now exports "+name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) > 0 {
		t.Fatalf("stale claudia-api-pin comments (🎯T1008):\n%s", strings.Join(stale, "\n"))
	}
}

// declDoc returns the doc comment and declared name for the forms this
// ratchet cares about: a single func/type, or the first spec in a const
// or var block (jevons's "does not export" claims are always singular
// declarations, not grouped blocks of unrelated constants).
func declDoc(decl ast.Decl) (*ast.CommentGroup, string) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Doc, d.Name.Name
	case *ast.GenDecl:
		if d.Doc == nil || len(d.Specs) == 0 {
			return nil, ""
		}
		switch s := d.Specs[0].(type) {
		case *ast.TypeSpec:
			return d.Doc, s.Name.Name
		case *ast.ValueSpec:
			if len(s.Names) == 0 {
				return nil, ""
			}
			return d.Doc, s.Names[0].Name
		}
	}
	return nil, ""
}

// pinnedClaudiaExports resolves the pinned claudia module (GOWORK=off, so
// a development sibling checkout under ../go.work cannot hide a stale
// pin — 🎯T448) and returns the set of exported top-level identifiers
// (func, type, const, var) across its non-test .go files.
func pinnedClaudiaExports(t *testing.T, root string) map[string]bool {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "github.com/marcelocantos/claudia")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("GOWORK=off go list claudia dir: %v", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("empty claudia module dir")
	}

	exported := map[string]bool{}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read claudia module dir %s: %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil {
					// Methods are reached through their type, not by
					// bare name; a stand-in comment names a package-level
					// symbol it duplicates, never a method alone.
					continue
				}
				if d.Name.IsExported() {
					exported[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							exported[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								exported[n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	return exported
}
