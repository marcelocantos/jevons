// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/capacity"
)

func TestT781InjectPackageParallelGoTest(t *testing.T) {
	got := injectPackageParallel([]string{"go", "test", "./..."}, 4)
	want := []string{"go", "test", "-p", "4", "./..."}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Already present: leave alone.
	kept := injectPackageParallel([]string{"go", "test", "-p", "2", "./internal/gate"}, 4)
	if strings.Join(kept, " ") != "go test -p 2 ./internal/gate" {
		t.Fatalf("existing -p was rewritten: %v", kept)
	}
	// Non-test go command: no injection.
	build := injectPackageParallel([]string{"go", "build", "./..."}, 4)
	if strings.Join(build, " ") != "go build ./..." {
		t.Fatalf("go build gained -p: %v", build)
	}
}

func TestT781InjectPackageParallelGotest(t *testing.T) {
	got := injectPackageParallel([]string{"gotest", "-timeout", "45m", "./..."}, 4)
	if strings.Join(got, " ") != "gotest -p 4 -timeout 45m ./..." {
		t.Fatalf("gotest injection = %v", got)
	}
}

func TestT781InjectPackageParallelLeavesMakeAlone(t *testing.T) {
	got := injectPackageParallel([]string{"make", "test-go"}, 4)
	if strings.Join(got, " ") != "make test-go" {
		t.Fatalf("make argv rewritten: %v", got)
	}
}

func TestT781WrapNice(t *testing.T) {
	got := wrapNice([]string{"go", "test", "./..."}, 5)
	if strings.Join(got, " ") != "nice -n 5 -- go test ./..." {
		t.Fatalf("wrapNice = %v", got)
	}
}

// A live heavy gate must put GOMAXPROCS on the child and, for go test, a -p.
// The child prints what it actually received so the oracle is the process,
// not the parent's bookkeeping.
func TestT781HeavyGateAppliesCPUBudget(t *testing.T) {
	t.Setenv(HeavySuiteUnlimitedEnv, "")
	t.Setenv(HeavySuiteMaxProcsEnv, "4")
	// Isolate from the owner's capacity.json so the knob under test is the one
	// we just set, not a leftover daily override.
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A tiny Go main that prints GOMAXPROCS and a stand-in for "am I under
	// budget". We drive it through `go test` shape via a helper script that
	// go test would not, because compiling a fixture package is heavier than
	// this check needs. Instead: run `go env GOMAXPROCS` under a heavy-shaped
	// argv? HeavyCommand requires `go test`. So use:
	//   go test -c -o /dev/null  with a tiny package — too heavy.
	// Use sh is NOT heavy. Use a real `go test` of a one-file package.
	dir := t.TempDir()
	pkg := filepath.Join(dir, "p")
	if err := os.Mkdir(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package p
import (
  "fmt"
  "os"
  "runtime"
  "testing"
)
func TestBudget(t *testing.T) {
  fmt.Printf("GOMAXPROCS=%d\n", runtime.GOMAXPROCS(0))
  fmt.Printf("ENV_GOMAXPROCS=%s\n", os.Getenv("GOMAXPROCS"))
  fmt.Printf("ENV_BUDGET=%s\n", os.Getenv("` + HeavySuiteMaxProcsEnv + `"))
}
`
	if err := os.WriteFile(filepath.Join(pkg, "p_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Need a go.mod so `go test` in an arbitrary dir resolves.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/p\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	rec, err := Run(&RunArgs{
		Command: []string{"go", "test", "-count=1", "./p"},
		Dir:     dir,
		Store:   store,
		Stdout:  &out,
		Stderr:  io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Verdict != VerdictGreen {
		t.Fatalf("verdict=%s status=%s out=%q", rec.Verdict, rec.Status(), out.String())
	}
	body := out.String()
	if !strings.Contains(body, "ENV_GOMAXPROCS=4") {
		t.Fatalf("child did not see GOMAXPROCS=4: %s", body)
	}
	if !strings.Contains(body, "ENV_BUDGET=4") {
		t.Fatalf("child did not see budget env: %s", body)
	}
	// runtime.GOMAXPROCS(0) should also be 4 (or less if the host is tiny).
	want := 4
	if c := runtime.NumCPU(); c < want {
		want = c
	}
	if !strings.Contains(body, "GOMAXPROCS="+strconv.Itoa(want)) {
		t.Fatalf("runtime GOMAXPROCS not budgeted (want %d): %s", want, body)
	}
}

// The unlimited escape must leave GOMAXPROCS alone (no forced budget).
func TestT781UnlimitedSkipsBudget(t *testing.T) {
	t.Setenv(HeavySuiteUnlimitedEnv, "1")
	t.Setenv(HeavySuiteMaxProcsEnv, "2")
	cmd := exec.Command("true")
	argv := []string{"go", "test", "./..."}
	before := append([]string(nil), argv...)
	applyHeavySuiteBudget(cmd, argv)
	if cmd.Env != nil {
		for _, e := range cmd.Env {
			if strings.HasPrefix(e, "GOMAXPROCS=") || strings.HasPrefix(e, HeavySuiteMaxProcsEnv+"=") {
				t.Fatalf("unlimited run still set %s", e)
			}
		}
	}
	// Args must stay unset / unchanged — apply starts from exec.Command("true")
	// and must not rewrite it when unlimited.
	if cmd.Path != "" && filepath.Base(cmd.Path) != "true" && filepath.Base(cmd.Args[0]) != "true" {
		// On some platforms Path is absolute; base must still be true.
		if filepath.Base(cmd.Path) != "true" {
			t.Fatalf("unlimited rewrite touched Path=%q Args=%v", cmd.Path, cmd.Args)
		}
	}
	_ = before
}

func TestT781ResolveMatchesCapacityKnob(t *testing.T) {
	t.Setenv(HeavySuiteUnlimitedEnv, "")
	t.Setenv(HeavySuiteMaxProcsEnv, "6")
	b := resolveHeavySuiteBudget()
	want := capacity.ResolveHeavySuiteBudget(&capacity.Policy{HeavySuiteMaxProcs: 6}, runtime.NumCPU())
	if b != want {
		t.Fatalf("resolve = %+v, want %+v", b, want)
	}
}
