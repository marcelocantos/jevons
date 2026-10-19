// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package claudiapin

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MissingRef is one jevons use of a claudia API the published pin lacks.
type MissingRef struct {
	// Kind is "top" (claudia.Name) or "field" (claudia.Type{ Field: ... }).
	Kind string
	// Type is the struct type for field refs; empty for top-level.
	Type string
	// Name is the missing identifier or field.
	Name string
	// File is repo-relative path.
	File string
	// Line is 1-based.
	Line int
}

// SymbolReport is the 🎯T608 surface check: every claudia.X selector and
// every keyed field of a claudia.T{…} composite in jevons source must
// exist on the published pin. A miss is a silent T360 red until named.
type SymbolReport struct {
	PinVersion string
	Missing    []MissingRef
}

// CheckSymbols scans repoRoot's non-test Go sources for claudia API use
// against the published pin (GOWORK=off).
func CheckSymbols(repoRoot string) (SymbolReport, error) {
	idx, err := LoadPinnedExports(repoRoot)
	if err != nil {
		return SymbolReport{}, err
	}
	return CheckSymbolsAgainst(repoRoot, idx)
}

// CheckSymbolsAgainst is the pure half: given an ExportIndex, scan
// repoRoot and report missing refs. Used by tests with fixture indexes.
func CheckSymbolsAgainst(repoRoot string, idx ExportIndex) (SymbolReport, error) {
	r := SymbolReport{PinVersion: idx.PinVersion}
	fset := token.NewFileSet()
	seen := map[string]bool{} // dedupe kind|type|name|file|line

	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			switch name {
			case ".git", "bin", "build", ".build", "node_modules", "ui", "ios", "macos",
				"testdata", "_scratchpad", "vendor":
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Skip generated / mirror paths that are not product imports.
		rel, rerr := filepath.Rel(repoRoot, path)
		if rerr != nil {
			rel = path
		}
		if strings.HasPrefix(rel, "ui"+string(filepath.Separator)) {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// Fast path: no claudia import string → skip parse.
		if !strings.Contains(string(src), ModulePath) && !strings.Contains(string(src), `"github.com/marcelocantos/claudia"`) {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, src, 0)
		if perr != nil {
			// Unbuildable scratch is not a pin-gap claim.
			return nil
		}
		aliases := claudiaImportNames(f)
		if len(aliases) == 0 {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok || !aliases[id.Name] {
					return true
				}
				name := x.Sel.Name
				if !ast.IsExported(name) {
					return true
				}
				if idx.Top[name] {
					return true
				}
				pos := fset.Position(x.Sel.Pos())
				addMissing(&r, seen, MissingRef{
					Kind: "top",
					Name: name,
					File: rel,
					Line: pos.Line,
				})
			case *ast.CompositeLit:
				typeName, ok := claudiaTypeName(x.Type, aliases)
				if !ok {
					return true
				}
				fields := idx.Fields[typeName]
				for _, elt := range x.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || !key.IsExported() {
						continue
					}
					if fields != nil && fields[key.Name] {
						continue
					}
					// Type not a struct (or pin has no fields map) and
					// field unknown → missing. Also missing when the
					// type itself is absent from the pin.
					if !idx.Top[typeName] || fields == nil || !fields[key.Name] {
						pos := fset.Position(key.Pos())
						addMissing(&r, seen, MissingRef{
							Kind: "field",
							Type: typeName,
							Name: key.Name,
							File: rel,
							Line: pos.Line,
						})
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return r, err
	}
	sort.Slice(r.Missing, func(i, j int) bool {
		a, b := r.Missing[i], r.Missing[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Name < b.Name
	})
	return r, nil
}

func addMissing(r *SymbolReport, seen map[string]bool, m MissingRef) {
	key := fmt.Sprintf("%s|%s|%s|%s|%d", m.Kind, m.Type, m.Name, m.File, m.Line)
	if seen[key] {
		return
	}
	seen[key] = true
	r.Missing = append(r.Missing, m)
}

func claudiaImportNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path != ModulePath {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				// Dot-import would need unqualified lookup; blank is unused.
				// Neither appears in jevons product code; skip rather than
				// false-negative the whole file.
				continue
			}
			out[imp.Name.Name] = true
			continue
		}
		out["claudia"] = true
	}
	return out
}

func claudiaTypeName(expr ast.Expr, aliases map[string]bool) (string, bool) {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		id, ok := t.X.(*ast.Ident)
		if !ok || !aliases[id.Name] {
			return "", false
		}
		return t.Sel.Name, true
	case *ast.StarExpr:
		return claudiaTypeName(t.X, aliases)
	default:
		return "", false
	}
}

// FormatSymbols renders a human block for CLI / test failure text.
// When Missing is non-empty the message names the pin version so the
// failure is a declared release gate, not an ordinary undefined: error.
func FormatSymbols(r SymbolReport) string {
	var b strings.Builder
	if len(r.Missing) == 0 {
		fmt.Fprintf(&b, "claudia symbols: pin=%s — jevons references are all on the published pin (🎯T608)\n",
			orDash(r.PinVersion))
		return b.String()
	}
	fmt.Fprintf(&b, "CLAUDIA PIN GAP (🎯T608): go.mod pins %s, which does not export API jevons already calls.\n",
		orDash(r.PinVersion))
	b.WriteString("This is a release gate, not ordinary breakage: publish a claudia release that includes the symbols, bump the go.mod pin, or drop the references. When the pin catches up, this check goes green with no further edits.\n")
	b.WriteString("missing:\n")
	limit := len(r.Missing)
	if limit > 40 {
		limit = 40
	}
	for _, m := range r.Missing[:limit] {
		switch m.Kind {
		case "field":
			fmt.Fprintf(&b, "  - %s:%d: claudia.%s field %s\n", m.File, m.Line, m.Type, m.Name)
		default:
			fmt.Fprintf(&b, "  - %s:%d: claudia.%s\n", m.File, m.Line, m.Name)
		}
	}
	if len(r.Missing) > limit {
		fmt.Fprintf(&b, "  … +%d more\n", len(r.Missing)-limit)
	}
	return b.String()
}

// SymbolsHardFail is true when jevons references API absent from the pin.
func SymbolsHardFail(r SymbolReport) bool {
	return len(r.Missing) > 0
}
