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
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 🎯T1090: daemon code resolves state under the configured state_dir, not
// under $HOME/.jevons. A function that calls os.UserHomeDir() and names a
// ".jevons" path component is resolving the development daemon's tree no
// matter which daemon runs it — an isolate with its own state_dir included.
// That is how the jobs registry and the roles overlay read and wrote the
// development tree from journey daemons.
//
// This ratchet lists every such function in a package jevonsd links. A new
// one fails. Each remaining one is named with the reason it is not keyed
// off state_dir; an entry that no longer matches fails too, so the list
// only shrinks by deliberate commit.
var t1090HomeJevonsJoins = map[string]string{
	// Default() IS the definition of the development state_dir; every other
	// path is meant to derive from it.
	"internal/config/config.go:Default": "defines the default state_dir (~/.jevons)",
	// Machine-wide stores shared by every agent on the host, each with its
	// own env override for tests and isolates.
	"internal/attrib/attrib.go:DefaultRoot":        "host-wide attribution store; JEVONS_ATTRIB_DIR overrides",
	"internal/gate/record.go:DefaultStoreRoot":     "host-wide gate record store (🎯T1027 one namespace); env override",
	"internal/treeguard/store.go:DefaultStoreRoot": "host-wide treeguard store; env override",
	"internal/spool/spool.go:Dir":                  "host-wide claudia spool; JEVONS_SPOOL_DIR overrides (🎯T1085 owns spool reads)",
	// The attribution hook runs per tool call in every worker, outside any
	// daemon, and reads the development registry to name the session.
	"internal/attrib/attrib.go:DefaultRosterPaths": "attribution CLI/hook reads the development registry; not a daemon state path",
	"internal/attrib/hook.go:LookupAgent":          "attribution hook reads the development registry; not a daemon state path",
	// Owner-run one-shot modes of the jevonsd binary that exit before the
	// daemon starts.
	"cmd/jevonsd/main.go:runOneShot":       "--pair / --add-credential one-shot; ignores state_dir (known gap, named in 🎯T1090's report)",
	"internal/uidaemon/install.go:Install": "-install-ui-agents owner install; Spec.StateDir overrides the default",
}

func TestT1090NoNewHomeJevonsJoinsInDaemonCode(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "list", "-deps", "-f",
		`{{if .Module}}{{if eq .Module.Path "github.com/marcelocantos/jevons"}}{{.Dir}}{{"\t"}}{{join .GoFiles " "}}{{end}}{{end}}`,
		"./cmd/jevonsd")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps ./cmd/jevonsd: %v", err)
	}

	found := map[string]bool{}
	fset := token.NewFileSet()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		dir, files, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		for _, name := range strings.Fields(files) {
			path := filepath.Join(dir, name)
			rel, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", rel, err)
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if t1090JoinsHomeJevons(fn.Body) {
					found[filepath.ToSlash(rel)+":"+t1090FuncName(fn)] = true
				}
			}
		}
	}

	var unlisted, stale []string
	for k := range found {
		if _, ok := t1090HomeJevonsJoins[k]; !ok {
			unlisted = append(unlisted, k)
		}
	}
	for k := range t1090HomeJevonsJoins {
		if !found[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	for _, k := range unlisted {
		t.Errorf("%s joins os.UserHomeDir() with .jevons: root it at the daemon's state_dir instead (🎯T1090), or list it here with the reason it cannot be", k)
	}
	for _, k := range stale {
		t.Errorf("%s is listed but no longer joins os.UserHomeDir() with .jevons: delete the entry", k)
	}
	if len(found) < len(t1090HomeJevonsJoins)-len(stale) || len(found) == 0 {
		t.Fatalf("scan found %d functions; the scan itself is broken", len(found))
	}
}

// t1090JoinsHomeJevons reports whether body (including closures in it)
// both calls os.UserHomeDir and has a string literal naming a .jevons path
// component.
func t1090JoinsHomeJevons(body *ast.BlockStmt) bool {
	home, jevons := false, false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "UserHomeDir" {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" {
					home = true
				}
			}
		case *ast.BasicLit:
			if x.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(x.Value)
			if err != nil {
				return true
			}
			if v == ".jevons" || strings.HasPrefix(v, ".jevons/") ||
				strings.HasPrefix(v, "/.jevons") || strings.HasPrefix(v, "~/.jevons") {
				jevons = true
			}
		}
		return true
	})
	return home && jevons
}

func t1090FuncName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if idx, ok := typ.(*ast.IndexExpr); ok {
		typ = idx.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}
