// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package testreap

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperModeEnv makes TestHelperProcess act as the fixture test: arm (or
// not), start a detached child that names its temp dir, then end one way.
const helperModeEnv = "TESTREAP_HELPER_MODE"

// markerPrefix is the line the helper prints once its child is running.
const markerPrefix = "TESTREAP_ROOT "

// sweepWait bounds how long the census waits for survivors to die.
const sweepWait = 10 * time.Second

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperModeEnv)
	if mode == "" {
		t.Skip("helper for the testreap oracles, not a test")
	}
	var root string
	if mode == "noreap" {
		root = filepath.Dir(t.TempDir()) + string(os.PathSeparator)
	} else {
		root = Arm(t)
	}
	// argv[0] names the temp dir, as the fixtures' binaries and scripts do,
	// and Setsid puts the child out of the test's session and group the way
	// cmd/detach and the claudia broker put theirs.
	sleep := filepath.Join(t.TempDir(), "sleep")
	if err := os.Symlink("/bin/sleep", sleep); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(sleep, "300")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString(markerPrefix + root + "\n")
	// Hold until the parent has counted the child. The parent answers only
	// for the modes that end on their own; the rest end by -timeout or
	// SIGKILL while blocked here.
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	switch mode {
	case "pass":
	case "fail":
		t.Fatal("fixture failed")
	case "panic":
		panic("fixture panicked")
	default:
		t.Fatalf("unknown mode %q", mode)
	}
}

// survivors is the census: pids whose argv names root.
func survivors(t *testing.T, root string) []int {
	t.Helper()
	out, err := exec.Command("pgrep", "-f", regexp.QuoteMeta(root)).Output()
	if err != nil {
		return nil // pgrep exits 1 when nothing matches
	}
	var pids []int
	for f := range strings.FieldsSeq(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// runHelper runs the fixture test in mode, returns the temp root it reported
// once its detached child was up, and waits for the fixture binary to end.
func runHelper(t *testing.T, mode string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.timeout=3s", "-test.count=1")
	cmd.Env = append(os.Environ(), helperModeEnv+"="+mode)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// The helper fails, panics and times out on purpose. Its output is kept
	// out of this test's own unless this test fails, so a gate reading the
	// run does not mistake the fixture's panic for ours.
	var output bytes.Buffer
	cmd.Stderr = &output
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("%s helper output:\n%s", mode, output.String())
		}
	})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var root string
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if r, ok := strings.CutPrefix(sc.Text(), markerPrefix); ok {
			root = r
			break
		}
	}
	if root == "" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("%s: helper never reported its temp root", mode)
	}
	// The census must see the child alive before the end, or zero after
	// proves nothing.
	if len(survivors(t, root)) == 0 {
		t.Fatalf("%s: census saw no child under %s while the fixture ran", mode, root)
	}
	switch mode {
	case "pass", "fail", "panic":
		_, _ = stdin.Write([]byte("end\n"))
	case "kill", "noreap":
		_ = cmd.Process.Signal(syscall.SIGKILL)
	}
	// Drain to EOF before Wait: a timeout dump that fills the pipe would
	// otherwise block the helper from ever exiting.
	_, _ = io.Copy(&output, stdout)
	_ = cmd.Wait()
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

// TestArmReapsOnEveryExit is the 🎯T932 oracle: whichever way the test
// binary ends — pass, fail, panic, -timeout, SIGKILL — no process naming its
// temp root survives, including one detached into its own session.
func TestArmReapsOnEveryExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process census")
	}
	for _, mode := range []string{"pass", "fail", "panic", "timeout", "kill"} {
		t.Run(mode, func(t *testing.T) {
			root := runHelper(t, mode)
			deadline := time.Now().Add(sweepWait)
			for left := survivors(t, root); len(left) > 0; left = survivors(t, root) {
				if time.Now().After(deadline) {
					t.Fatalf("%s: %d process(es) naming %s outlived the test: %v", mode, len(left), root, left)
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}

// TestUnarmedFixtureLeaks is the control: the same fixture without Arm,
// killed, leaves its detached child behind — so the census above can see a
// leak when there is one.
func TestUnarmedFixtureLeaks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process census")
	}
	root := runHelper(t, "noreap")
	time.Sleep(500 * time.Millisecond)
	left := survivors(t, root)
	for _, pid := range left {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if len(left) == 0 {
		t.Fatalf("unarmed fixture left nothing under %s; the census cannot detect a leak", root)
	}
}
