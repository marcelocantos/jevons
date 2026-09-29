// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// minSourceToolNames is the floor on tool names the source walk finds: 56 on
// 2026-09-29. A walk that finds none checks nothing, so it must not pass.
const minSourceToolNames = 56

// 🎯T908: every tool constructor in the module names its tool with a string
// literal that reaches the Anthropic API intact under the seat-side `_`
// prefix. Walking the source rather than one server's tools/list covers
// tools registered only when a Set* wire runs.
func TestT908EveryToolNameInSourceMatchesAnthropicPattern(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var names []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "mcp" ||
				(sel.Sel.Name != "NewTool" && sel.Sel.Name != "NewToolWithRawSchema") {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: mcp.%s name is not a string literal; the T908 ratchet cannot check it",
					fset.Position(call.Pos()), sel.Sel.Name)
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Errorf("%s: %v", fset.Position(lit.Pos()), err)
				return true
			}
			names = append(names, name)
			if !validToolName(name) {
				t.Errorf("%s: tool %q reaches the Anthropic API as %q, outside %s",
					fset.Position(lit.Pos()), name, seatToolNamePrefix+name, anthropicToolName)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < minSourceToolNames {
		t.Fatalf("source walk found %d tool names; want at least %d", len(names), minSourceToolNames)
	}
}

// The registered surface, as tools/list serves it, with the self-test wire
// on. The two tools that were dotted are listed under their new names.
func TestT908RegisteredToolsMatchAnthropicPattern(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.SetSelfTestEnv(nil)
	tools := s.mcpSrv.ListTools()
	if len(tools) == 0 {
		t.Fatal("server registered no tools")
	}
	for name := range tools {
		if !validToolName(name) {
			t.Errorf("registered tool %q reaches the Anthropic API as %q, outside %s",
				name, seatToolNamePrefix+name, anthropicToolName)
		}
	}
	for _, want := range []string{"self_test_run", "self_test_list"} {
		if tools[want] == nil {
			t.Errorf("%s is not registered", want)
		}
	}
}

// The guard in addTool keeps a bad name off tools/list; the control is the
// same tool under a valid name.
func TestT908AddToolRefusesNameOutsidePattern(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.addTool(mcp.NewTool("t908.dotted"), nil)
	s.addTool(mcp.NewTool("t908_plain"), nil)
	tools := s.mcpSrv.ListTools()
	if tools["t908.dotted"] != nil {
		t.Error("addTool registered t908.dotted")
	}
	if tools["t908_plain"] == nil {
		t.Error("addTool did not register the control t908_plain")
	}
}

// The check is against the name as the seat sends it: `_` prefix included,
// so the limit on the bare name is 127.
func TestT908ValidToolNameCountsSeatPrefix(t *testing.T) {
	for name, want := range map[string]bool{
		"self_test_run":          true,
		"jevons_agent_list":      true,
		"a-b":                    true,
		"self_test.run":          false,
		"self_test.list":         false,
		"jevons agent":           false,
		strings.Repeat("x", 127): true,
		strings.Repeat("x", 128): false,
	} {
		if got := validToolName(name); got != want {
			t.Errorf("validToolName(%q) = %v, want %v", name, got, want)
		}
	}
}
