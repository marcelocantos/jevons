// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// 🎯T766.4.1: the reap, stop, and idle decisions do not decide by English.
//
// TestT7664ReapDoneEnglishAllowlistZero (internal/mcpserver) scans a
// hand-listed set of function bodies for five string calls. It never looks
// at callees, so a decision that calls a helper that calls a regexp passes
// it — which is exactly how gate.FlagFalseGreen's prose rules stayed a reap
// veto through the 2026-10-19 burst. This ratchet asks the question
// structurally: build the module's call graph (SSA + VTA, so calls through
// function values and interfaces resolve to what actually flows there),
// walk every function transitively reachable from the decision functions,
// and fail on any call into a text matcher.
//
// A text matcher is any regexp call, or a strings call that finds, splits,
// or compares substrings (strings.Contains, HasPrefix, Index, Cut, Fields,
// Split, EqualFold, …). Normalising calls (TrimSpace, ToLower, Join) are not
// matchers.
//
// Two kinds of reader may match text, and neither is listed by hand:
//
//   - The envelope fence grammar: every function in internal/envelope
//     reachable from envelope.Parse. That is how a decision reads a report —
//     slot lines in the fence, never the payload. Other envelope functions
//     (ClassifyProgress, OracleBody, which do read prose) get no exemption.
//   - Readers of inputs that are not report text, named below with what
//     they read. Growing that list is a deliberate edit.
//
// Effects after a decision — delivering the parent notice, which travels
// the notify path where the report-time FALSE-GREEN banner lives — are not
// decisions and are not roots.

const jevonsModule = "github.com/marcelocantos/jevons"

// t76641NonReportReaders match strings that are not a report.
var t76641NonReportReaders = map[string]string{
	jevonsModule + "/internal/agentreport.Load":         "validates a store file name and agent name, not the report body",
	jevonsModule + "/internal/agentreport.List":         "filters report-store file names by suffix",
	jevonsModule + "/internal/agentreport.safeAgentDir": "rejects path separators in an agent name",
	jevonsModule + "/internal/gate.countDirty":          "splits `git status --porcelain` output",
}

var t76641Matchers = map[string]bool{
	"Contains": true, "ContainsAny": true, "ContainsRune": true, "ContainsFunc": true,
	"HasPrefix": true, "HasSuffix": true, "EqualFold": true,
	"Index": true, "IndexAny": true, "IndexByte": true, "IndexRune": true, "IndexFunc": true,
	"LastIndex": true, "LastIndexAny": true, "LastIndexByte": true, "LastIndexFunc": true,
	"Cut": true, "CutPrefix": true, "CutSuffix": true,
	"Fields": true, "FieldsFunc": true, "FieldsSeq": true, "FieldsFuncSeq": true,
	"Split": true, "SplitN": true, "SplitAfter": true, "SplitAfterN": true,
	"SplitSeq": true, "SplitAfterSeq": true, "Lines": true,
	"Count": true, "TrimPrefix": true, "TrimSuffix": true,
}

type t76641Graph struct {
	prog *ssa.Program
	cg   *callgraph.Graph
	pkgs map[string]*ssa.Package
}

func loadT76641Graph(t *testing.T) *t76641Graph {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Dir: repoRoot(t),
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedSyntax | packages.NeedTypesSizes,
	}, "./internal/...", "./cmd/...")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			t.Fatalf("%s: %v", p.PkgPath, e)
		}
	}
	prog, ssaPkgs := ssautil.Packages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	g := &t76641Graph{prog: prog, cg: vta.CallGraph(ssautil.AllFunctions(prog), nil), pkgs: map[string]*ssa.Package{}}
	for _, p := range ssaPkgs {
		if p != nil {
			g.pkgs[p.Pkg.Path()] = p
		}
	}
	return g
}

func (g *t76641Graph) fn(t *testing.T, pkg, name string) *ssa.Function {
	t.Helper()
	p := g.pkgs[jevonsModule+"/"+pkg]
	if p == nil || p.Func(name) == nil {
		t.Fatalf("%s.%s not found", pkg, name)
	}
	return p.Func(name)
}

func (g *t76641Graph) method(t *testing.T, pkg, recv, name string) *ssa.Function {
	t.Helper()
	p := g.pkgs[jevonsModule+"/"+pkg]
	if p == nil {
		t.Fatalf("package %s not built", pkg)
	}
	obj := p.Pkg.Scope().Lookup(recv)
	if obj == nil {
		t.Fatalf("type %s.%s not found", pkg, recv)
	}
	sel := g.prog.MethodSets.MethodSet(types.NewPointer(obj.Type())).Lookup(p.Pkg, name)
	if sel == nil {
		t.Fatalf("method (*%s).%s not found", recv, name)
	}
	return g.prog.MethodValue(sel)
}

// reach walks from roots through module functions and returns every
// function reached, with the caller each was first reached from.
func (g *t76641Graph) reach(roots []*ssa.Function) map[*ssa.Function]*ssa.Function {
	via := map[*ssa.Function]*ssa.Function{}
	queue := append([]*ssa.Function(nil), roots...)
	for _, r := range roots {
		via[r] = nil
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		node := g.cg.Nodes[f]
		if node == nil {
			continue
		}
		for _, e := range node.Out {
			callee := e.Callee.Func
			if _, seen := via[callee]; seen {
				continue
			}
			via[callee] = f
			if t76641InModule(callee) {
				queue = append(queue, callee)
			}
		}
	}
	return via
}

// matcherCalls reports each reached module function's calls into a text
// matcher, outside the exempt readers, with the chain that reached it.
func (g *t76641Graph) matcherCalls(roots []*ssa.Function, grammar map[*ssa.Function]bool) []string {
	via := g.reach(roots)
	var hits []string
	for f := range via {
		if !t76641InModule(f) || grammar[f] {
			continue
		}
		if _, ok := t76641NonReportReaders[t76641Name(f)]; ok {
			continue
		}
		node := g.cg.Nodes[f]
		if node == nil {
			continue
		}
		for _, e := range node.Out {
			callee := e.Callee.Func
			if !t76641IsMatcher(callee) {
				continue
			}
			chain := []string{callee.RelString(nil)}
			for at := f; at != nil; at = via[at] {
				chain = append(chain, at.RelString(nil))
			}
			for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
				chain[i], chain[j] = chain[j], chain[i]
			}
			hits = append(hits, strings.Join(chain, " → "))
		}
	}
	sort.Strings(hits)
	return hits
}

func t76641IsMatcher(f *ssa.Function) bool {
	if f.Pkg == nil {
		// A method of *regexp.Regexp or *strings.Builder has no package of
		// its own here; its receiver names it.
		if recv := f.Signature.Recv(); recv != nil {
			if named, ok := types.Unalias(derefT76641(recv.Type())).(*types.Named); ok && named.Obj().Pkg() != nil {
				return named.Obj().Pkg().Path() == "regexp"
			}
		}
		return false
	}
	switch f.Pkg.Pkg.Path() {
	case "regexp":
		return true
	case "strings":
		return f.Signature.Recv() == nil && t76641Matchers[f.Name()]
	}
	return false
}

func derefT76641(t types.Type) types.Type {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

func t76641Name(f *ssa.Function) string {
	if f.Pkg == nil {
		return f.String()
	}
	return f.Pkg.Pkg.Path() + "." + f.Name()
}

// t76641InModule is true for module code, including the synthetic wrappers
// SSA makes for it (bound method values, thunks, instantiations), which
// have no package of their own: a method value passed as a func is reached
// through its wrapper, and the walk must go through it.
func t76641InModule(f *ssa.Function) bool {
	for at := f; at != nil; at = at.Parent() {
		if at.Pkg != nil {
			return strings.HasPrefix(at.Pkg.Pkg.Path(), jevonsModule+"/")
		}
		if at.Origin() != nil && at.Origin() != at {
			return t76641InModule(at.Origin())
		}
		if obj := at.Object(); obj != nil && obj.Pkg() != nil {
			return strings.HasPrefix(obj.Pkg().Path(), jevonsModule+"/")
		}
	}
	return false
}

func TestT76641ReapDecisionsReachNoProseMatcher(t *testing.T) {
	g := loadT76641Graph(t)
	// The envelope fence grammar: what envelope.Parse reaches inside its
	// own package.
	grammar := map[*ssa.Function]bool{}
	for f := range g.reach([]*ssa.Function{g.fn(t, "internal/envelope", "Parse")}) {
		if f.Pkg != nil && f.Pkg.Pkg.Path() == jevonsModule+"/internal/envelope" || f.Parent() != nil && f.Parent().Pkg != nil && f.Parent().Pkg.Pkg.Path() == jevonsModule+"/internal/envelope" {
			grammar[f] = true
		}
	}
	if len(grammar) < 10 {
		t.Fatalf("envelope grammar = %d functions; the exemption is not what this test thinks it is", len(grammar))
	}
	const mcp = "internal/mcpserver"
	roots := []*ssa.Function{
		// reap
		g.fn(t, mcp, "ShouldAutoReapDoneWorkAgent"), // reaches its gate veto
		g.fn(t, mcp, "outstandingScopeReasonsForWorker"),
		// stop
		g.method(t, mcp, "Server", "stopDispositionFor"),
		g.fn(t, mcp, "ClassifyStopDisposition"),
		// idle
		g.fn(t, mcp, "ClassifyWorkerIdleDisposition"),
		g.fn(t, mcp, "storedReportLooksFinished"),
		g.method(t, mcp, "Server", "workerIdleSuppressReason"),
		g.fn(t, mcp, "DeclaresBlockingGateWait"),
		g.method(t, mcp, "IdleActivityTracker", "NoteTerminalTurn"),
	}
	if hits := g.matcherCalls(roots, grammar); len(hits) != 0 {
		t.Fatalf("reap/stop/idle decisions reach %d text matcher call(s) outside the envelope grammar (🎯T766.4.1):\n%s",
			len(hits), strings.Join(hits, "\n"))
	}

	// The walk follows function values: the reap's isDurable and the
	// veto's record lookup resolve to what production passes. Without
	// that, a prose classifier passed as a value would be invisible.
	via := g.reach(roots)
	for _, want := range []*ssa.Function{
		g.method(t, mcp, "Server", "durableSeat"),
		g.method(t, "internal/gate", "Store", "Lookup"),
	} {
		if _, ok := via[want]; !ok {
			t.Fatalf("%s is not reached through its function value; the call graph is not resolving dynamic calls", want.RelString(nil))
		}
	}
}

// TestT76641CallGraphRatchetSeesTheBanner is the control: from the
// report-time banner, the same walk does find the prose rules. If it
// stopped finding them, the ratchet above would pass for the wrong reason.
func TestT76641CallGraphRatchetSeesTheBanner(t *testing.T) {
	g := loadT76641Graph(t)
	hits := g.matcherCalls([]*ssa.Function{g.fn(t, "internal/mcpserver", "FalseGreenFlags")}, nil)
	found := false
	for _, h := range hits {
		if strings.Contains(h, "gate.FlagFalseGreen") && strings.Contains(h, "regexp") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the walk does not see gate.FlagFalseGreen's regexps from the banner (%d hits)", len(hits))
	}
}
