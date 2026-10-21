package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// resolveDaemon picks the jevonsd the isolate runs. Order: -bin, then the
// tree's bin/jevonsd, then a build of the tree's own ./cmd/jevonsd into
// bin/jevonsd (🎯T805). It never falls back to PATH: that is the released
// build, and in a clean worktree (bin/ is untracked) it silently made the
// journey measure the wrong product. Every build of the tree carries the
// control plane, so the tree binary needs no further check; `make
// test-journey` rebuilds it from the tree's sources before the suite runs.
func resolveDaemon(root, flagBin string, build func(dir, out string) error) (string, error) {
	if flagBin != "" {
		p, err := exec.LookPath(flagBin)
		if err != nil {
			return "", err
		}
		return filepath.Abs(p)
	}
	cand := filepath.Join(root, "bin", "jevonsd")
	if st, err := os.Stat(cand); err == nil && !st.IsDir() {
		return cand, nil
	}
	if err := os.MkdirAll(filepath.Dir(cand), 0o755); err != nil {
		return "", err
	}
	if err := build(root, cand); err != nil {
		return "", fmt.Errorf("no bin/jevonsd under %s and building ./cmd/jevonsd failed: %w", root, err)
	}
	return cand, nil
}

func daemonBuildCommand(dir, out string) *exec.Cmd {
	cmd := exec.Command("go", "build", "-o", out, "./cmd/jevonsd")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd
}

func goBuildDaemon(dir, out string) error {
	return daemonBuildCommand(dir, out).Run()
}
