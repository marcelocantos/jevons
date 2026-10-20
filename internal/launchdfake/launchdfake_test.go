// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package launchdfake

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

	"github.com/marcelocantos/jevons/internal/supervise"
)

const (
	modulePath = "github.com/marcelocantos/jevons"
	self       = modulePath + "/internal/launchdfake"
	seamPkg    = modulePath + "/internal/supervise"
	// seamFile is the one Go file allowed to exec launchctl.
	seamFile = "internal/supervise/launchctl.go"
)

// mayNameLaunchctl is every non-test Go file allowed to name launchctl,
// and why. Each entry is a file, not a package, so a new exec beside an
// allowed one still trips the ratchet.
var mayNameLaunchctl = map[string]string{
	seamFile:                        "the seam: the one launchctl exec in the module",
	"internal/testhome/testhome.go": "test-only: writes the stub launchctl children find on PATH",
}

// TestEveryTestBinaryLinkingSuperviseLinksLaunchdfake is the ratchet over
// the in-process seam. The set is every test binary the go command builds
// for this module whose dependencies include internal/supervise — whether
// it calls LoadAgent today or not, because a binary that links the seam is
// one refactor away from calling it, and a hand-kept list is how
// cmd/jevons-watchdog, cmd/jevonsd and internal/uidaemon were left out of
// 🎯T1095's fake.
//
// internal/supervise's own binary is the exception: the in-package fake in
// its export_test.go installs the same empty domain, and an in-package test
// file cannot import this package without an import cycle.
func TestEveryTestBinaryLinkingSuperviseLinksLaunchdfake(t *testing.T) {
	cmd := exec.Command("go", "list", "-e", "-test", "-f",
		`{{if and (eq .Name "main") (not .ForTest)}}{{.ImportPath}}{{range .Deps}} {{.}}{{end}}{{end}}`, "./...")
	cmd.Dir = moduleRoot(t)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	linking := 0
	var missing []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasSuffix(fields[0], ".test") {
			continue
		}
		pkg := strings.TrimSuffix(fields[0], ".test")
		links := map[string]bool{}
		for _, dep := range fields[1:] {
			links[dep] = true
		}
		if !links[seamPkg] || pkg == seamPkg {
			continue
		}
		linking++
		if !links[self] {
			missing = append(missing, pkg)
		}
	}
	if linking == 0 {
		t.Fatal("go list named no test binary linking internal/supervise; the census is broken, not clean")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("%d of %d test packages link internal/supervise without the launchd fake, so their "+
			"launchctl calls are refused instead of answered; add a _test.go file with\n"+
			"\timport _ %q\nto:\n  %s",
			len(missing), linking, self, strings.Join(missing, "\n  "))
	}
	t.Logf("%d test binaries link internal/supervise, all with the launchd fake", linking)
}

// TestTheSeamPackageInstallsItsOwnFake keeps the exception above honest:
// internal/supervise's test binary replaces the seam from an init in
// export_test.go.
func TestTheSeamPackageInstallsItsOwnFake(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "internal", "supervise", "export_test.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "init" || fn.Recv != nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			as, ok := stmt.(*ast.AssignStmt)
			if !ok {
				continue
			}
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == "launchctl" {
					return
				}
			}
		}
	}
	t.Fatalf("%s no longer assigns launchctl in init; its test binary would run on the refusing default", path)
}

// TestNoGoFileOutsideTheSeamExecsLaunchctl is the ratchet over direct
// execs: a launchctl run outside internal/supervise/launchctl.go is
// invisible to the fake. Non-test files may not name launchctl as a string
// at all (a command name handed to a runner is an exec too); test files
// may — they write stub launchctl scripts — but may not exec it.
func TestNoGoFileOutsideTheSeamExecsLaunchctl(t *testing.T) {
	root := moduleRoot(t)
	var offences []string
	files := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			if path != root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir // another module
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if _, ok := mayNameLaunchctl[rel]; ok {
			return nil
		}
		files++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil // not this ratchet's business; go vet reports it
		}
		isTest := strings.HasSuffix(path, "_test.go")
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if !isTest && namesLaunchctl(n) {
					offences = append(offences, fset.Position(n.Pos()).String()+": names launchctl outside the seam")
				}
			case *ast.CallExpr:
				if arg := execName(n); arg != nil && namesLaunchctl(arg) {
					offences = append(offences, fset.Position(n.Pos()).String()+": execs launchctl")
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("walked no Go files; the census is broken, not clean")
	}
	sort.Strings(offences)
	if len(offences) > 0 {
		for i, o := range offences {
			if rel, err := filepath.Rel(root, o); err == nil {
				offences[i] = rel
			}
		}
		t.Fatalf("launchctl outside %s, where no test fake can answer for it; use supervise.Launchctl "+
			"(or a helper beside it) and supervise.LaunchctlCommand:\n  %s", seamFile, strings.Join(offences, "\n  "))
	}
}

// namesLaunchctl reports whether a string literal is the launchctl command
// itself, or a shell line starting with it.
func namesLaunchctl(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	s = strings.TrimSpace(s)
	return s == "launchctl" || strings.HasPrefix(s, "launchctl ") || strings.HasSuffix(s, "/launchctl")
}

// execName returns the command-name argument of an exec.Command or
// exec.CommandContext call, or nil.
func execName(call *ast.CallExpr) ast.Expr {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "exec" {
		return nil
	}
	switch {
	case sel.Sel.Name == "Command" && len(call.Args) > 0:
		return call.Args[0]
	case sel.Sel.Name == "CommandContext" && len(call.Args) > 1:
		return call.Args[1]
	}
	return nil
}

// The init installed the recorder: a call through the seam is answered as
// an empty domain and recorded, and a mutation is refused.
func TestInitInstallsTheEmptyDomain(t *testing.T) {
	before := len(Calls())
	loaded, err := supervise.AgentLoaded(supervise.AgentLabel)
	if err != nil || loaded {
		t.Fatalf("AgentLoaded = %v, %v; want false, nil from an empty domain", loaded, err)
	}
	disabled, err := supervise.AgentDisabled(supervise.AgentLabel)
	if err != nil || disabled {
		t.Fatalf("AgentDisabled = %v, %v; want false, nil", disabled, err)
	}
	if err := supervise.KickstartAgent(supervise.DaemonLabel); err == nil {
		t.Fatal("kickstart succeeded against the fake; it must refuse every change")
	}
	got := Calls()[before:]
	if len(got) != 3 || got[0][0] != "print" || got[1][0] != "print-disabled" || got[2][0] != "kickstart" {
		t.Fatalf("recorded %q, want print, print-disabled, kickstart", got)
	}
}

// Use swaps the answer for one test and restores the empty domain after.
func TestUseAnswersForOneTest(t *testing.T) {
	t.Run("held", func(t *testing.T) {
		Use(t, func(args ...string) ([]byte, error) { return []byte("state = running"), nil })
		if loaded, err := supervise.AgentLoaded(supervise.AgentLabel); err != nil || !loaded {
			t.Fatalf("AgentLoaded = %v, %v under Use; want true, nil", loaded, err)
		}
	})
	if loaded, err := supervise.AgentLoaded(supervise.AgentLabel); err != nil || loaded {
		t.Fatalf("AgentLoaded = %v, %v after Use's test ended; want the empty domain back", loaded, err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "env", "GOMOD")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" || mod == os.DevNull {
		t.Fatal("not inside the jevons module")
	}
	return filepath.Dir(mod)
}
