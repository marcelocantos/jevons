// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"go/types"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// 🎯T1074.9: each control-plane database has one owner process. The Claudia
// broker alone opens claudia-control.db; jevonsd keeps its own
// control-replica.db and syncs with the broker over the control socket.
//
// The shared-file defect got in because an in-process harness was promoted
// to production: controlplane.SyncLocked joined two sqlpipe peers inside
// jevonsd with sqlpipe.SyncPeerHandshake, and controlplane.Open + Bind made
// jevonsd a writer of the broker's file. Rows arrived in both files, which a
// shared file also produces, so nothing noticed.
//
// This ratchet asks the topology question of the binaries themselves. For
// every main under cmd/ that links the control plane or sqlpipe, it builds
// the whole program (SSA, rapid type analysis from main and init) and fails
// if any function below is reachable, naming the call chain. It also fails
// on any reference, in a Jevons package those binaries link, to the
// broker's file name. Test code is not loaded; the fixture broker in
// internal/brokerfixture is linked by tests only.
//
// The live half — each file open in exactly one process, facts crossing
// the wire, drop/duplicate/reconnect — is
// internal/cost TestT10749TopologyTwoProcessesOverTheWire.

// t10749Unreachable are the functions no Jevons binary may reach.
var t10749Unreachable = map[string]string{
	"github.com/marcelocantos/sqlpipe/go/sqlpipe.SyncPeerHandshake": "sqlpipe's in-process two-peer harness",
	"github.com/marcelocantos/claudia/controlplane.SyncLocked":      "both peers of the pair in one process",
	"github.com/marcelocantos/claudia/controlplane.Bind":            "binds this process as a writer of the broker's file",
	"github.com/marcelocantos/claudia/controlplane.Open":            "opens the broker's file",
	"github.com/marcelocantos/claudia.OpenControlPublisher":         "opens the broker's file as its owner",
}

// controlplane.PublishFile is not listed: its package's init registers it as
// a function value (claudia.SetPublishControlFile), and RTA counts every
// address-taken function as reachable through reflect.Value.Call. The only
// call of that value is claudia.OpenControlPublisher, which is listed, and
// PublishFile itself refuses any process that is not the broker daemon.

// t10749BrokerFileNames are the spellings of the broker's file name.
var t10749BrokerFileNames = map[string]bool{
	"github.com/marcelocantos/claudia/controlplane.ClaudiaDBFile": true,
	"github.com/marcelocantos/claudia.ClaudiaDBFile":              true,
}

func TestT10749NoJevonsBinaryReachesTheBrokersFile(t *testing.T) {
	root := repoRoot(t)
	var flags []string
	if t10749NextBuild {
		flags = []string{"-tags=jevons_claudia_next"}
	}
	mains := t10749ControlPlaneMains(t, root, flags)
	if len(mains) == 0 {
		t.Fatal("no main under cmd/ links the control plane or sqlpipe; the ratchet would be vacuous")
	}
	pkgs, err := packages.Load(&packages.Config{
		Dir:        root,
		BuildFlags: flags,
		Mode:       packages.LoadAllSyntax,
	}, mains...)
	if err != nil {
		t.Fatal(err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		t.Fatal("package load errors")
	}

	// Name references, in Jevons packages these binaries link.
	var refs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if !strings.HasPrefix(p.PkgPath, jevonsModule) || p.TypesInfo == nil {
			return
		}
		for id, obj := range p.TypesInfo.Uses {
			if obj.Pkg() == nil {
				continue
			}
			full := obj.Pkg().Path() + "." + obj.Name()
			_, fn := t10749Unreachable[full]
			if t10749BrokerFileNames[full] || (fn && isPkgLevel(obj)) {
				refs = append(refs, p.Fset.Position(id.Pos()).String()+": "+full)
			}
		}
	})
	sort.Strings(refs)
	for _, r := range refs {
		t.Errorf("a Jevons binary's package names the broker's file or a shared-file call: %s", r)
	}

	// Reachability, through Claudia and sqlpipe as well as Jevons.
	prog, ssaPkgs := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	var roots []*ssa.Function
	for _, p := range ssaPkgs {
		if p == nil || p.Pkg.Name() != "main" {
			continue
		}
		for _, name := range []string{"main", "init"} {
			if f := p.Func(name); f != nil {
				roots = append(roots, f)
			}
		}
	}
	res := rta.Analyze(roots, true)
	reached := map[string]*ssa.Function{}
	for f := range res.Reachable {
		reached[t10749Name(f)] = f
	}
	for name, why := range t10749Unreachable {
		if f, ok := reached[name]; ok {
			t.Errorf("%s (%s) is reachable from a Jevons binary: %s", name, why, t10749Chain(res, f))
		}
	}
	// The positive control: the rule is not vacuous on a build that has a
	// consumer. Jevons' own replica and its link are reachable.
	if t10749NextBuild {
		for _, name := range []string{
			"github.com/marcelocantos/claudia/controlplane.OpenReplica",
			"(*github.com/marcelocantos/claudia/controlplane.Replica).Link",
		} {
			if _, ok := reached[name]; !ok {
				t.Errorf("%s is not reachable from jevonsd: the consumer is not wired, or this ratchet sees the wrong program", name)
			}
		}
	}
}

// t10749ControlPlaneMains lists the main packages under cmd/ that link the
// control plane or sqlpipe.
func t10749ControlPlaneMains(t *testing.T, root string, flags []string) []string {
	t.Helper()
	args := append([]string{"list"}, flags...)
	args = append(args, "-f", `{{if eq .Name "main"}}{{.ImportPath}}{{range .Deps}} {{.}}{{end}}{{end}}`, "./cmd/...")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var mains []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		for _, dep := range fields[1:] {
			if dep == "github.com/marcelocantos/claudia/controlplane" || dep == "github.com/marcelocantos/sqlpipe/go/sqlpipe" {
				mains = append(mains, fields[0])
				break
			}
		}
	}
	return mains
}

func isPkgLevel(obj types.Object) bool {
	return obj.Parent() == obj.Pkg().Scope()
}

// t10749Name is a function's package-qualified name, with the receiver for
// a method: "(*pkg.T).M".
func t10749Name(f *ssa.Function) string {
	if recv := f.Signature.Recv(); recv != nil {
		return "(" + types.TypeString(recv.Type(), nil) + ")." + f.Name()
	}
	if f.Pkg == nil {
		return f.String()
	}
	return f.Pkg.Pkg.Path() + "." + f.Name()
}

// t10749Chain walks the RTA call graph back from f to a root.
func t10749Chain(res *rta.Result, f *ssa.Function) string {
	chain := []string{f.String()}
	seen := map[*ssa.Function]bool{f: true}
	for at := res.CallGraph.Nodes[f]; at != nil && len(at.In) > 0; {
		caller := at.In[0].Caller.Func
		if seen[caller] {
			break
		}
		seen[caller] = true
		chain = append(chain, caller.String())
		at = res.CallGraph.Nodes[caller]
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return strings.Join(chain, " → ")
}
