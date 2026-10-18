package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveDaemon picks the jevonsd the isolate runs. Order: -bin, then a
// tree bin/jevonsd that was built with jevons_claudia_next, then a build
// of the tree's own ./cmd/jevonsd (🎯T805). An untagged bin/jevonsd is the
// published-pin stand-in: OpenReplica fails closed and the isolate cannot
// read a receipt. It never falls back to PATH.
func resolveDaemon(root, flagBin string, build func(dir, out string) error) (string, error) {
	if flagBin != "" {
		p, err := exec.LookPath(flagBin)
		if err != nil {
			return "", err
		}
		return filepath.Abs(p)
	}
	cand := filepath.Join(root, "bin", "jevonsd")
	if daemonCarriesControlPlane(cand) {
		return cand, nil
	}
	if err := os.MkdirAll(filepath.Dir(cand), 0o755); err != nil {
		return "", err
	}
	if err := build(root, cand); err != nil {
		return "", fmt.Errorf("building ./cmd/jevonsd with the control plane failed: %w", err)
	}
	return cand, nil
}

// daemonCarriesControlPlane reports whether path is a Go binary built with
// jevons_claudia_next. go version -m is the build record; a missing file
// or a binary without that tag does not carry the control plane.
func daemonCarriesControlPlane(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	out, err := exec.Command("go", "version", "-m", path).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "jevons_claudia_next")
}

func daemonBuildCommand(dir, out string) *exec.Cmd {
	cmd := exec.Command("go", "build", "-tags", "jevons_claudia_next", "-o", out, "./cmd/jevonsd")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd
}

func goBuildDaemon(dir, out string) error {
	if err := daemonBuildCommand(dir, out).Run(); err != nil {
		return err
	}
	if !daemonCarriesControlPlane(out) {
		return fmt.Errorf("%s built without jevons_claudia_next", out)
	}
	return nil
}
