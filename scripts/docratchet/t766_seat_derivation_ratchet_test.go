// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// Observation-only files may consume raw signals. Controls are never feed
// exemptions. Return types identify phase decoders, so changing their names
// (including the old PhaseFromFile rename) does not change the result.
var seatObservationFiles = map[string]bool{
	"internal/mcpserver/seat_observations.go": true,
	"internal/seatactivity/activity.go":       true,
	"internal/server/seat_observations.go":    true,
	"internal/mcpserver/session_phase.go":     true,
}

type seatSourceFile struct {
	rel     string
	tree    *ast.File
	imports map[string]string
}

func seatDerivationSites(root string) ([]string, error) {
	fs := token.NewFileSet()
	files := []seatSourceFile{}
	// Package-qualified declarations returning raw evidence. Calls from controls
	// are forbidden even when the implementation lives in an allowlisted feed.
	evidence := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			f, err := parser.ParseFile(fs, path, nil, 0)
			if err != nil {
				return err
			}
			imports := map[string]string{}
			for _, imp := range f.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				alias := filepath.Base(p)
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				imports[alias] = p
			}
			pkg := "github.com/marcelocantos/jevons/" + filepath.ToSlash(filepath.Dir(rel))
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Type.Results == nil {
					continue
				}
				for _, result := range fn.Type.Results.List {
					raw := false
					if id, ok := result.Type.(*ast.Ident); ok {
						raw = (filepath.Dir(rel) == "internal/turnev" && id.Name == "Phase") || (filepath.Dir(rel) == "internal/panecensus" && fn.Recv == nil && id.Name == "Flight") || (seatObservationFiles[rel] && (id.Name == "birthDiagnosis" || id.Name == "SessionEvidence" || (filepath.Dir(rel) == "internal/seatactivity" && id.Name == "Reading") || (id.Name == "string" && fn.Name.Name != "AgentTranscriptPath")))
					}
					if sel, ok := result.Type.(*ast.SelectorExpr); ok {
						if id, ok := sel.X.(*ast.Ident); ok {
							raw = seatObservationFiles[rel] && strings.HasSuffix(imports[id.Name], "/turnev") && sel.Sel.Name == "Phase"
						}
					}
					if raw {
						evidence[pkg+"."+fn.Name.Name] = true
					}
				}
			}
			files = append(files, seatSourceFile{rel, f, imports})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	var sites []string
	for _, file := range files {
		if strings.HasPrefix(file.rel, "internal/seatstate/") || seatObservationFiles[file.rel] || strings.HasPrefix(file.rel, "internal/turnev/") {
			continue
		}
		pkg := "github.com/marcelocantos/jevons/" + filepath.ToSlash(filepath.Dir(file.rel))
		ast.Inspect(file.tree, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			forbidden := false
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				forbidden = evidence[pkg+"."+fn.Name]
			case *ast.SelectorExpr:
				// These are external provider APIs, not Jevons private helper names.
				switch fn.Sel.Name {
				case "SessionExists":
					forbidden = true
				case "Alive", "PromptInFlight", "TurnPhase", "ProcessAlive", "Provider":
					forbidden = len(call.Args) == 0
				case "Model":
					forbidden = len(call.Args) == 0 && filepath.Dir(file.rel) != "internal/desktop" && filepath.Dir(file.rel) != "cmd/jevons-head"
				}
				target := pkg
				if id, ok := fn.X.(*ast.Ident); ok && file.imports[id.Name] != "" {
					target = file.imports[id.Name]
				}
				forbidden = forbidden || evidence[target+"."+fn.Sel.Name]
			}
			if forbidden {
				sites = append(sites, fs.Position(call.Pos()).String())
			}
			return true
		})
	}
	sort.Strings(sites)
	return sites, nil
}

func TestT766SeatStateDerivationsOnlyFall(t *testing.T) {
	sites, err := seatDerivationSites(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 0 {
		t.Fatalf("%d raw seat derivation call sites outside observation feeds (pin 0):\n%s", len(sites), strings.Join(sites, "\n"))
	}
}

func TestT766RatchetRejectsRenamedWrappersAndDecoders(t *testing.T) {
	for _, renamed := range []string{"oldName", "completelyDifferentName"} {
		t.Run(renamed, func(t *testing.T) {
			root := t.TempDir()
			sources := map[string]string{
				"internal/turnev/phase.go":  "package turnev; type Phase int; func " + renamed + "(path string) Phase { return 0 }",
				"internal/moved/control.go": "package moved; import renamedImport \"github.com/marcelocantos/jevons/internal/turnev\"; func " + renamed + "(p interface{Alive()bool}) bool { return p.Alive() }; func control(){ _ = renamedImport." + renamed + "(\"session\") }",
				"cmd/main.go":               "package main",
			}
			for p, src := range sources {
				path := filepath.Join(root, p)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(src), 0644); err != nil {
					t.Fatal(err)
				}
			}
			sites, err := seatDerivationSites(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(sites) != 2 {
				t.Fatalf("renamed/moved raw reads escaped: %v", sites)
			}
		})
	}
}

// Go's type checker also catches field reads and method-value aliases, which
// a call-name scan cannot see. In particular BrokerSeat.Alive used to bypass
// the process-method ratchet while still authorizing a stop.
func TestT766RawProviderSelectionsOnlyInFeeds(t *testing.T) {
	root := repoRoot(t)
	pkgs, err := packages.Load(&packages.Config{Dir: root, Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedImports}, "./internal/...", "./cmd/...")
	if err != nil {
		t.Fatal(err)
	}
	var sites []string
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			t.Error(err)
		}
		if pkg.TypesInfo == nil {
			continue
		}
		for expr, sel := range pkg.TypesInfo.Selections {
			if !rawProviderSelection(sel) {
				continue
			}
			pos := pkg.Fset.Position(expr.Pos())
			rel, err := filepath.Rel(root, pos.Filename)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(rel, "internal/seatstate/") || seatObservationFiles[rel] {
				continue
			}
			sites = append(sites, pos.String())
		}
	}
	sort.Strings(sites)
	if len(sites) > 0 {
		t.Fatalf("raw provider field/method selections outside feeds (pin 0):\n%s", strings.Join(sites, "\n"))
	}
}

// Desired AgentDef/Config fields are configuration, not observed process truth.
// Actual Agent methods and BrokerSeat condition fields are raw observations.
func rawProviderSelection(sel *types.Selection) bool {
	typ := sel.Recv()
	if p, ok := typ.(*types.Pointer); ok {
		typ = p.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "github.com/marcelocantos/claudia" {
		return false
	}
	switch named.Obj().Name() {
	case "Agent", "BrokerSeat":
	default:
		return false
	}
	switch sel.Obj().Name() {
	case "Alive", "ProcessAlive", "PromptInFlight", "TurnPhase", "Provider", "Model", "Owned":
		return true
	}
	return false
}

type seatTestImporter struct{ pkg *types.Package }

func (i seatTestImporter) Import(string) (*types.Package, error) { return i.pkg, nil }

func TestT766TypedRatchetRejectsAliasesAndRenamedWrappers(t *testing.T) {
	fs := token.NewFileSet()
	raw, err := parser.ParseFile(fs, "provider.go", `package claudia
type Agent struct{}
func (*Agent) Alive() bool { return true }
type BrokerSeat struct { Alive bool }
type AgentDef struct { Model string }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := new(types.Config).Check("github.com/marcelocantos/claudia", fs, []*ast.File{raw}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"original", "renamedAgain"} {
		f, err := parser.ParseFile(fs, name+".go", `package control
import alias "github.com/marcelocantos/claudia"
func `+name+`(a *alias.Agent, b alias.BrokerSeat, desired alias.AgentDef) bool {
	bound := a.Alive
	_ = desired.Model
	return bound() || b.Alive
}`, 0)
		if err != nil {
			t.Fatal(err)
		}
		info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
		cfg := types.Config{Importer: seatTestImporter{provider}}
		if _, err := cfg.Check("control", fs, []*ast.File{f}, info); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, sel := range info.Selections {
			if rawProviderSelection(sel) {
				count++
			}
		}
		if count != 2 {
			t.Fatalf("%s: raw selections=%d, want 2", name, count)
		}
	}
}

// Feed exemptions are observation-only; moving an actuator into one is not a
// way to satisfy the zero-control-derivation pin.
func TestT766ObservationFilesCannotActuateSeats(t *testing.T) {
	fs := token.NewFileSet()
	for rel := range seatObservationFiles {
		f, err := parser.ParseFile(fs, filepath.Join(repoRoot(t), rel), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Send", "SendAsync", "Interrupt", "Stop", "Kill", "Launch", "Adopt", "Remove":
				t.Errorf("observation feed actuates a seat: %s", fs.Position(call.Pos()))
			}
			return true
		})
	}
}
