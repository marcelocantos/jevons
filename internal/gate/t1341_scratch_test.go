// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// A real subprocess dies while RunClean is blocked in the gate command. No
// defer in that process gets to run; the daemon's sweep must finish the job.
func TestT1341KilledGateScratchIsSwept(t *testing.T) {
	if os.Getenv("T1341_HELPER") == "1" {
		_, _ = RunClean(&CleanArgs{Repo: os.Getenv("T1341_REPO"), WorkDir: os.Getenv("T1341_TEMP"), Command: []string{"sh", "-c", "touch ready; exec sleep 120"}})
		os.Exit(0)
	}
	root, _ := fixtureRepo(t, true)
	temp := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^TestT1341KilledGateScratchIsSwept$")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.Env = append(os.Environ(), "T1341_HELPER=1", "T1341_REPO="+root, "T1341_TEMP="+temp)
	var output strings.Builder
	child.Stdout, child.Stderr = &output, &output
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL); _ = child.Wait() }()
	var scratch string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(temp, "gate-clean-*", "tree", "ready"))
		if len(matches) > 0 {
			scratch = filepath.Dir(filepath.Dir(matches[0]))
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if scratch == "" {
		t.Fatalf("gate did not start: %s", output.String())
	}
	if n, err := worktreereap.SweepGateScratch(root, temp, false); err != nil || n != 0 {
		t.Fatalf("swept live gate: n=%d err=%v", n, err)
	}
	if err := syscall.Kill(-child.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if n, err := worktreereap.SweepGateScratch(root, temp, false); err != nil || n != 1 {
		t.Fatalf("failed to sweep dead gate: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch still exists: %v", err)
	}
	out := fixtureGit(t, root, "worktree", "list", "--porcelain")
	if strings.Contains(out, scratch) {
		t.Fatalf("registration still exists: %s", out)
	}
}
