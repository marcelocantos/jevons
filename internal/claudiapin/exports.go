// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package claudiapin

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExportIndex is the published pin's API surface: top-level exported
// identifiers and, per exported struct type, its exported fields.
// Methods are omitted — callers reach them through a typed receiver,
// and the load-bearing 🎯T608 failure mode is package-level selectors
// plus keyed composite-literal fields (SandboxWritableRoots et al.).
type ExportIndex struct {
	// PinVersion is the go.mod require (e.g. v0.53.0).
	PinVersion string
	// ModuleDir is the resolved module cache (or replace) directory.
	ModuleDir string
	// Top holds exported package-level names (func, type, const, var).
	Top map[string]bool
	// Fields maps type name → exported field names of that struct.
	Fields map[string]map[string]bool
}

// ResolvePinModuleDir returns the directory of the pinned claudia module
// under GOWORK=off so a sibling checkout via ../go.work cannot stand in
// for the published pin (🎯T448 / 🎯T608).
func ResolvePinModuleDir(repoRoot string) (version, dir string, err error) {
	modBody, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "", "", fmt.Errorf("read go.mod: %w", err)
	}
	version = RequireVersion(string(modBody))
	if version == "" {
		return "", "", fmt.Errorf("go.mod has no require %s", ModulePath)
	}
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", ModulePath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		return version, "", fmt.Errorf("GOWORK=off go list %s: %w", ModulePath, err)
	}
	dir = strings.TrimSpace(string(out))
	if dir == "" {
		return version, "", fmt.Errorf("empty module dir for %s@%s", ModulePath, version)
	}
	return version, dir, nil
}

// LoadExports parses the pin module directory into an ExportIndex.
func LoadExports(pinVersion, moduleDir string) (ExportIndex, error) {
	idx := ExportIndex{
		PinVersion: pinVersion,
		ModuleDir:  moduleDir,
		Top:        map[string]bool{},
		Fields:     map[string]map[string]bool{},
	}
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return idx, fmt.Errorf("read pin module %s: %w", moduleDir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(moduleDir, name), nil, 0)
		if perr != nil {
			return idx, fmt.Errorf("parse %s: %w", name, perr)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil {
					continue
				}
				if d.Name.IsExported() {
					idx.Top[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if !s.Name.IsExported() {
							continue
						}
						idx.Top[s.Name.Name] = true
						st, ok := s.Type.(*ast.StructType)
						if !ok || st.Fields == nil {
							continue
						}
						fm := idx.Fields[s.Name.Name]
						if fm == nil {
							fm = map[string]bool{}
							idx.Fields[s.Name.Name] = fm
						}
						for _, field := range st.Fields.List {
							for _, n := range field.Names {
								if n.IsExported() {
									fm[n.Name] = true
								}
							}
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								idx.Top[n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	if len(idx.Top) == 0 {
		return idx, fmt.Errorf("pin module %s has no exported top-level identifiers", moduleDir)
	}
	return idx, nil
}

// LoadPinnedExports resolves and parses the published pin for repoRoot.
func LoadPinnedExports(repoRoot string) (ExportIndex, error) {
	ver, dir, err := ResolvePinModuleDir(repoRoot)
	if err != nil {
		return ExportIndex{}, err
	}
	return LoadExports(ver, dir)
}
