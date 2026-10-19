// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// 🎯T1079: seatprovider.Resolve is Jevons' one answer to which provider a
// seat runs on. The overseer row stored the plan name grok while every
// sidecar check knew only launch names (xai-oauth), and a restarted overseer
// read phase_unknown (🎯T1077). A census of the sites that compared would
// have been stale the day after it was written, so this is a typed rule
// over every non-test file in cmd/ and internal/:
//
//   - a comparison (==, !=, switch, case) or a strings / slices matcher
//     with an operand of type claudia.Provider is raw: the stored string,
//     in whichever vocabulary its writer used;
//   - so is one whose operand is a string constant naming a provider in
//     either vocabulary ("grok", "xai-oauth", string(claudia.ProviderClaude));
//   - so is one whose operand is a plain-string variable or field named
//     for a provider (agentInfo.Provider, sw.FromProvider), seen through
//     TrimSpace / ToLower and conversions.
//
//   - so is a call to claudia.PlanProvider or claudia.SubscriptionSeatProvider
//     (a second resolver, case-sensitive and blind to Jevons' aliases), and a
//     conversion to seatprovider.Plan (a Plan minted without being read).
//
// Comparing seatprovider.Plan values (Resolve(p).Plan == seatprovider.Grok)
// and asking Identity.Sidecar are the forms that remain. Only
// internal/seatprovider itself may read the raw names. An empty-string
// comparison asks whether a provider is set at all and is not a reading of
// its meaning; a substring matcher with a provider-named constant needle
// (strings.Contains(model, "claude")) sniffs a model id and is judged by its
// haystack.
//
// Residue the rule does not decide: a map indexed by a provider string (the
// planusage tables key on canonical plan ids they resolved first; capacity
// load is keyed by Plan since this target), and a raw string held in a
// variable whose name does not say provider (cost's dest/cfg were fixed by
// hand). Test files and scripts/ are outside it.

const (
	claudiaPkg      = "github.com/marcelocantos/claudia"
	seatproviderPkg = "github.com/marcelocantos/jevons/internal/seatprovider"
)

var providerNamedVar = regexp.MustCompile(`(?i)provider$`)

type providerComparisonChecker struct {
	fset  *token.FileSet
	info  *types.Info
	sites []string
}

func isClaudiaProvider(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == claudiaPkg && obj.Name() == "Provider"
}

func isPlainString(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && (b.Kind() == types.String || b.Kind() == types.UntypedString)
}

// calleeName is pkg.Func for a package-level function call, else "".
func (c *providerComparisonChecker) calleeName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	fn, ok := c.info.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return ""
	}
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		return ""
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// unwrap looks through parentheses, conversions and the string normalisers
// a raw comparison hides behind.
func (c *providerComparisonChecker) unwrap(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
			continue
		case *ast.CallExpr:
			if len(x.Args) == 1 {
				if tv, ok := c.info.Types[x.Fun]; ok && tv.IsType() {
					e = x.Args[0]
					continue
				}
				switch c.calleeName(x) {
				case "strings.TrimSpace", "strings.ToLower", "strings.ToUpper":
					e = x.Args[0]
					continue
				}
			}
		}
		return e
	}
}

// emptyString reports a constant "" operand.
func (c *providerComparisonChecker) emptyString(e ast.Expr) bool {
	tv, ok := c.info.Types[e]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) == ""
}

// raw reports whether e is a provider name read as a string.
func (c *providerComparisonChecker) raw(e ast.Expr) bool { return c.rawOperand(e, true) }

// rawOperand is raw with the constant clause optional: a substring matcher
// (strings.Contains(model, "claude")) sniffing a model id for a vendor word
// is not reading a provider; its haystack is what decides.
func (c *providerComparisonChecker) rawOperand(e ast.Expr, constants bool) bool {
	for _, x := range []ast.Expr{e, c.unwrap(e)} {
		tv, ok := c.info.Types[x]
		if !ok {
			continue
		}
		if tv.Value != nil && tv.Value.Kind() == constant.String {
			if !constants {
				continue
			}
			// A constant of another named type (seatprovider.Plan,
			// harnessusage.Harness) already went through a parser.
			if (isPlainString(tv.Type) || isClaudiaProvider(tv.Type)) &&
				seatprovider.Resolve(claudia.Provider(constant.StringVal(tv.Value))).Known {
				return true
			}
			continue
		}
		if isClaudiaProvider(tv.Type) {
			return true
		}
		if !isPlainString(tv.Type) {
			continue
		}
		switch v := x.(type) {
		case *ast.Ident:
			if providerNamedVar.MatchString(v.Name) {
				return true
			}
		case *ast.SelectorExpr:
			if providerNamedVar.MatchString(v.Sel.Name) {
				return true
			}
		}
	}
	return false
}

func (c *providerComparisonChecker) flag(n ast.Node) {
	c.sites = append(c.sites, c.fset.Position(n.Pos()).String())
}

func (c *providerComparisonChecker) file(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op != token.EQL && x.Op != token.NEQ {
				return true
			}
			if c.emptyString(x.X) || c.emptyString(x.Y) {
				return true
			}
			if c.raw(x.X) || c.raw(x.Y) {
				c.flag(x)
			}
		case *ast.SwitchStmt:
			if x.Tag == nil {
				return true
			}
			if c.raw(x.Tag) {
				c.flag(x)
				return true
			}
			for _, stmt := range x.Body.List {
				for _, e := range stmt.(*ast.CaseClause).List {
					if c.raw(e) {
						c.flag(e)
					}
				}
			}
		case *ast.CallExpr:
			// seatprovider.Plan(raw) mints a Plan without reading it: the
			// typed comparisons below would then trust a raw string.
			if tv, ok := c.info.Types[x.Fun]; ok && tv.IsType() {
				if named, ok := tv.Type.(*types.Named); ok && named.Obj().Pkg() != nil &&
					named.Obj().Pkg().Path() == seatproviderPkg && named.Obj().Name() == "Plan" {
					c.flag(x)
				}
			}
			switch c.calleeName(x) {
			case claudiaPkg + ".PlanProvider", claudiaPkg + ".SubscriptionSeatProvider":
				// A second resolver: case-sensitive and blind to the
				// aliases Resolve reads.
				c.flag(x)
			case "strings.EqualFold", "strings.Compare", "slices.Contains", "slices.Index":
				for _, a := range x.Args {
					if c.raw(a) {
						c.flag(x)
						break
					}
				}
			case "strings.HasPrefix", "strings.HasSuffix", "strings.Contains", "strings.Index":
				for _, a := range x.Args {
					if c.rawOperand(a, false) {
						c.flag(x)
						break
					}
				}
			}
		}
		return true
	})
}

func rawProviderComparisonSites(t *testing.T, root string) []string {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Dir:  root,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedImports,
	}, "./internal/...", "./cmd/...")
	if err != nil {
		t.Fatal(err)
	}
	var sites []string
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			t.Error(err)
		}
		if pkg.TypesInfo == nil || pkg.PkgPath == seatproviderPkg {
			continue
		}
		c := &providerComparisonChecker{fset: pkg.Fset, info: pkg.TypesInfo}
		for _, f := range pkg.Syntax {
			if strings.HasSuffix(pkg.Fset.Position(f.Pos()).Filename, "_test.go") {
				continue
			}
			c.file(f)
		}
		for _, s := range c.sites {
			if rel, err := filepath.Rel(root, s); err == nil {
				s = rel
			}
			sites = append(sites, s)
		}
	}
	sort.Strings(sites)
	return sites
}

func TestT1079NoRawProviderComparisons(t *testing.T) {
	sites := rawProviderComparisonSites(t, repoRoot(t))
	if len(sites) > 0 {
		t.Fatalf("%d raw provider comparisons outside internal/seatprovider (pin 0); compare seatprovider.Resolve(p).Plan or ask .Sidecar:\n%s",
			len(sites), strings.Join(sites, "\n"))
	}
}

type providerStubImporter struct{ pkgs map[string]*types.Package }

func (i providerStubImporter) Import(path string) (*types.Package, error) { return i.pkgs[path], nil }

// The rule sees the forms the census missed: a renamed variable of the
// claudia type, a plan or launch name hidden behind a conversion or a
// normaliser, a string field named for a provider, a switch on either, and
// a strings matcher. It leaves a non-provider string and an empty check.
func TestT1079RatchetRejectsEveryRawForm(t *testing.T) {
	fs := token.NewFileSet()
	check := func(path, src string, imports map[string]*types.Package, info *types.Info) *types.Package {
		t.Helper()
		f, err := parser.ParseFile(fs, path+".go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		pkg, err := (&types.Config{Importer: providerStubImporter{imports}}).Check(path, fs, []*ast.File{f}, info)
		if err != nil {
			t.Fatal(err)
		}
		return pkg
	}
	strs := check("strings", `package strings
func TrimSpace(s string) string { return s }
func ToLower(s string) string { return s }
func EqualFold(a, b string) bool { return a == b }
func Contains(s, sub string) bool { return s == sub }`, nil, nil)
	sp := check(seatproviderPkg, `package seatprovider
type Plan string`, nil, nil)
	cl := check(claudiaPkg, `package claudia
type Provider string
const ProviderGrok Provider = "grok"
type AgentDef struct { Provider Provider }`, nil, nil)
	src := `package control
import (
	alias "github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"strings"
)
type row struct { Provider, Name string }
func f(d alias.AgentDef, renamed alias.Provider, r row, other string) bool {
	_ = d.Provider == alias.ProviderGrok                       // 1 typed
	_ = renamed != "anything"                                  // 2 typed, renamed
	_ = strings.ToLower(string(d.Provider)) == other           // 3 typed under a normaliser
	_ = other == "xai-oauth"                                   // 4 launch name constant
	_ = other == string(alias.ProviderGrok)                    // 5 plan name constant
	_ = strings.TrimSpace(r.Provider) == other                 // 6 field named for a provider
	switch renamed {                                           // 7 switch on the type
	}
	switch other {
	case "anthropic":                                          // 8 case constant
	}
	_ = strings.EqualFold(other, "codex")                      // 9 matcher
	_ = strings.Contains(string(d.Provider), other)            // 10 substring of a provider
	_ = strings.Contains(other, "claude")                      // a model id sniffed for a vendor word: allowed
	_ = seatprovider.Plan(other)                               // 11 a Plan minted from a raw string
	_ = d.Provider == ""                                       // empty check: allowed
	_ = r.Name == other                                        // not a provider: allowed
	return other == "jevons"                                   // not a provider name: allowed
}`
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	f, err := parser.ParseFile(fs, "control.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&types.Config{Importer: providerStubImporter{map[string]*types.Package{"strings": strs, claudiaPkg: cl, seatproviderPkg: sp}}}).Check("control", fs, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	c := &providerComparisonChecker{fset: fs, info: info}
	c.file(f)
	if len(c.sites) != 11 {
		t.Fatalf("ratchet flagged %d sites, want the 11 raw forms:\n%s", len(c.sites), strings.Join(c.sites, "\n"))
	}
}
