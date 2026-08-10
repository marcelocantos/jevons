// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/gate"
)

// These exercise the shipped binary, because the guarantee 🎯T386 and 🎯T396
// make is about what a worker actually types. A package-level API that is
// honest while the command wrapping it loses the status would close nothing.

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

func gateBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gate-bin")
		if err != nil {
			buildErr = err
			return
		}
		builtBin = filepath.Join(dir, "gate")
		cmd := exec.Command("go", "build", "-o", builtBin, "./cmd/gate")
		cmd.Dir = repoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = err
			t.Logf("go build ./cmd/gate: %s", out)
		}
	})
	if buildErr != nil {
		t.Fatalf("building cmd/gate: %v", buildErr)
	}
	return builtBin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}

// runGate invokes the built binary against a private store and returns its
// exit code and combined output.
func runGate(t *testing.T, storeDir string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(gateBinary(t), args...)
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+storeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, isExit := errAsExit(err); !isExit {
			t.Fatalf("running gate %v: %v (%s)", args, err, out)
		}
	}
	return cmd.ProcessState.ExitCode(), string(out)
}

func errAsExit(err error) (*exec.ExitError, bool) {
	e, ok := err.(*exec.ExitError)
	return e, ok
}

// 🎯T396 acceptance 1 through the shipped command: gate exits with the gate's
// own status, so it drops into a Makefile recipe or an && chain unchanged.
func TestGateCLIPropagatesTheCommandsStatus(t *testing.T) {
	store := t.TempDir()

	code, out := runGate(t, store, "--", "sh", "-c", "echo failing >&2; exit 1")
	if code != 1 {
		t.Fatalf("gate exited %d for a command that exited 1\n%s", code, out)
	}
	if !strings.Contains(out, "exit=1 RED") {
		t.Fatalf("attestation missing from output:\n%s", out)
	}

	code, out = runGate(t, store, "--", "sh", "-c", "echo fine; exit 0")
	if code != 0 {
		t.Fatalf("gate exited %d for a passing command\n%s", code, out)
	}
	if !strings.Contains(out, "exit=0 GREEN") {
		t.Fatalf("passing gate did not attest green:\n%s", out)
	}
}

// A zero status over output that shows a timeout is not something the CLI
// will hand on as success — it exits 3 (SUSPECT), which fails an && chain.
func TestGateCLIRefusesToPassOnASuspectGreen(t *testing.T) {
	store := t.TempDir()

	code, out := runGate(t, store, "--", "sh", "-c",
		`echo "panic: test timed out after 10m0s"; exit 0`)
	if code != 3 {
		t.Fatalf("gate exited %d for a suspect green, want 3\n%s", code, out)
	}
	if !strings.Contains(out, "SUSPECT") || !strings.Contains(out, "output contradicts") {
		t.Fatalf("suspect run did not say why:\n%s", out)
	}

	// The deliberate override still reports the verdict; it only stops the
	// exit code from failing the chain.
	code, out = runGate(t, store, "-allow-suspect", "--", "sh", "-c",
		`echo "panic: test timed out after 10m0s"; exit 0`)
	if code != 0 {
		t.Fatalf("-allow-suspect exited %d, want the command's own 0\n%s", code, out)
	}
	if !strings.Contains(out, "SUSPECT") {
		t.Fatalf("-allow-suspect hid the verdict:\n%s", out)
	}
}

// 🎯T396 acceptance 2: a background gate carries the same guarantee. The
// process's status is written to the record by the process's own parent, so
// a harness that relays "exit code 0" for a run that exited 1 is contradicted
// by evidence on disk — which is the in-band read-back the workaround-turned-
// doctrine ("redirect to a file and read $?") was reaching for.
func TestGateCLIBackgroundRunIsReadableInBand(t *testing.T) {
	store := t.TempDir()

	cmd := exec.Command(gateBinary(t), "-quiet", "--", "sh", "-c", "echo bg >&2; exit 1")
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting background gate: %v", err)
	}
	_ = cmd.Wait() // a non-zero status here is the expected outcome, not an error

	// Read the status the way a worker would after the fact, ignoring
	// whatever the harness claimed about the process.
	code, out := runGate(t, store, "last")
	if code == 0 {
		t.Fatalf("gate last reported success for a failed background run:\n%s", out)
	}
	if !strings.Contains(out, "exit=1 RED") {
		t.Fatalf("gate last lost the background status:\n%s", out)
	}
}

// The report-time half, wired to the store: `gate check` reads a finish
// report on stdin and flags a green it cannot support.
func TestGateCLICheckFlagsAFalseGreen(t *testing.T) {
	store := t.TempDir()

	report := "🎯T414 finished, tests pass.\n\n" +
		"    $ go test ./... 2>&1 | tail -20\n" +
		"    --- FAIL: TestThreadResume (0.02s)\n" +
		"    $ echo \"exit=$?\"\n    exit=0\n"

	cmd := exec.Command(gateBinary(t), "check")
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	cmd.Stdin = strings.NewReader(report)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("gate check exited 0 on a false green:\n%s", out)
	}
	if code := cmd.ProcessState.ExitCode(); code != 4 {
		t.Fatalf("gate check exited %d, want 4\n%s", code, out)
	}
	if !strings.Contains(string(out), string(gate.FlagPipelineMasked)) {
		t.Fatalf("gate check did not name the pipeline masking:\n%s", out)
	}

	// And stays quiet on an honest one.
	cmd = exec.Command(gateBinary(t), "check")
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	cmd.Stdin = strings.NewReader("Still working; go test is running.")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gate check flagged an honest report: %v\n%s", err, out)
	}
}
