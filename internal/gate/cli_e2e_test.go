// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// These exercise the shipped binary, because the guarantee 🎯T386 and 🎯T396
// make is about what a worker actually types. A package-level API that is
// honest while the command wrapping it loses the status would close nothing.

var (
	buildOnce   sync.Once
	builtBin    string
	builtBinDir string
	buildErr    error
)

// TestMain removes the gate-bin* directory built once for the whole package
// run (🎯T1087): without this, every `go test` invocation left a built
// binary behind in $TMPDIR forever.
func TestMain(m *testing.M) {
	code := m.Run()
	if builtBinDir != "" {
		os.RemoveAll(builtBinDir)
	}
	os.Exit(code)
}

func gateBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gate-bin")
		if err != nil {
			buildErr = err
			return
		}
		builtBinDir = dir
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

// 🎯T461: bin/gate last / show print KILLED rather than RED, and check refuses
// citing that run as a pass.
func TestGateCLIT461KilledVerdictOnLastShowCheck(t *testing.T) {
	store := t.TempDir()

	code, out := runGate(t, store, "-quiet", "--", "sh", "-c", "kill -9 $$")
	if code == 0 {
		t.Fatalf("gate exited 0 for a SIGKILL'd command\n%s", out)
	}
	if !strings.Contains(out, "KILLED") {
		t.Fatalf("gate run did not attest KILLED:\n%s", out)
	}
	if strings.Contains(out, " RED") {
		t.Fatalf("SIGKILL was reported as RED:\n%s", out)
	}

	code, out = runGate(t, store, "last")
	if code == 0 {
		t.Fatalf("gate last exited 0 for a killed run\n%s", out)
	}
	if !strings.Contains(out, "KILLED") {
		t.Fatalf("gate last lost the killed verdict:\n%s", out)
	}

	// Pull the id out of the attestation for show.
	id := ""
	for _, part := range strings.Fields(out) {
		if strings.HasPrefix(part, "id=") {
			id = strings.TrimPrefix(part, "id=")
			break
		}
	}
	if id == "" {
		t.Fatalf("no id in last output:\n%s", out)
	}
	code, out = runGate(t, store, "show", id)
	if code == 0 {
		t.Fatalf("gate show exited 0 for a killed run\n%s", out)
	}
	if !strings.Contains(out, "KILLED") {
		t.Fatalf("gate show lost the killed verdict:\n%s", out)
	}

	report := "🎯T461 done, make test-go green.\n\n    GATE kill-shell exit=unknown KILLED id=" + id + " out=aaaaaaaaaaaa dur=0s\n"
	cmd := exec.Command(gateBinary(t), "check")
	cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	cmd.Stdin = strings.NewReader(report)
	checkOut, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("gate check exited 0 on a killed citation:\n%s", checkOut)
	}
	if !strings.Contains(string(checkOut), string(gate.FlagAttestationKilled)) &&
		!strings.Contains(string(checkOut), "KILLED") {
		t.Fatalf("gate check did not refuse the killed citation:\n%s", checkOut)
	}
}

// 🎯T1132: when the wrapper that invoked bin/gate dies, the gate and its
// command tree stop within 2s and the record is KILLED caller_gone.
func TestGateCLIT1132CallerGoneKillsOrphanTree(t *testing.T) {
	bin := gateBinary(t)
	store := t.TempDir()

	marker := fmt.Sprintf("t1132-%d", time.Now().UnixNano())
	// Background the gate so killing the shell (as timeout does to its child
	// chain) leaves an orphan gate process reparented to launchd.
	script := fmt.Sprintf("%q -quiet -- sh -c 'sleep 300 # %s' & exec sleep 3600", bin, marker)
	wrapper := exec.Command("sh", "-c", script)
	wrapper.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
	if err := wrapper.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := syscall.Kill(wrapper.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = wrapper.Wait()

	deadline := time.Now().Add(5 * time.Second)
	var lastOut string
	for time.Now().Before(deadline) {
		code, out := runGate(t, store, "last")
		lastOut = out
		if strings.Contains(out, "KILLED") && strings.Contains(out, "caller_gone") {
			if code == 0 {
				t.Fatalf("gate last exited 0 for caller_gone:\n%s", out)
			}
			if procs, err := exec.Command("pgrep", "-f", marker).Output(); err == nil && len(strings.TrimSpace(string(procs))) > 0 {
				t.Fatalf("gate recorded caller_gone but command tree still alive: %s", procs)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("gate did not record KILLED caller_gone within 5s; last=\n%s", lastOut)
}
