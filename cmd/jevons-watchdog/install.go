// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// installAgent writes the LaunchAgent and loads it.
//
// It deliberately does not touch `brew services`. The Cellar install has
// its own launchd job with KeepAlive and is already supervised; this one
// watches the daily repo daemon on the same port, and the restart script
// it invokes stops the brew service before taking the port, exactly as
// it always has. Installing this changes nothing about the brew path.
func installAgent(repo string, port int, state string) int {
	if runtime.GOOS != "darwin" {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: -install is macOS/launchd only (this is %s).\n"+
			"On other platforms run the same binary from systemd, cron, or any supervisor on a %ds interval.\n",
			runtime.GOOS, supervise.AgentInterval)
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	repo, err = filepath.Abs(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	binary := filepath.Join(repo, "bin", "jevons-watchdog")
	if !executable(binary) {
		// Pointing launchd at a path that is not there installs a job
		// that fails every 30 seconds and supervises nothing.
		fmt.Fprintf(os.Stderr, "jevons-watchdog: no binary at %s — run `make` first\n", binary)
		return 1
	}
	script := filepath.Join(repo, "scripts", "restart-daily-jevonsd.sh")
	if !executable(script) {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: no restart script at %s — is -repo right?\n", script)
		return 1
	}

	spec := supervise.AgentSpec{
		Binary:   binary,
		Repo:     repo,
		StateDir: state,
		Port:     port,
		LogPath:  supervise.AgentLogPath(state),
	}
	if err := os.MkdirAll(supervise.Dir(state), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	path := supervise.AgentPlistPath(home)
	if err := supervise.WriteAgentPlist(path, spec); err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	fmt.Printf("wrote %s\n", path)

	domain := fmt.Sprintf("gui/%d", os.Getuid())
	// bootout first so a reinstall picks up new arguments; a job that is
	// not loaded makes this fail, which is not an error here.
	_ = exec.Command("launchctl", "bootout", domain+"/"+supervise.AgentLabel).Run()
	if out, err := exec.Command("launchctl", "bootstrap", domain, path).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: launchctl bootstrap: %v: %s\n", err, strings.TrimSpace(string(out)))
		return 1
	}
	out, err := exec.Command("launchctl", "print", domain+"/"+supervise.AgentLabel).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: loaded, but launchctl print failed: %v\n", err)
		return 1
	}
	fmt.Printf("loaded %s (every %ds, log %s)\n", supervise.AgentLabel, supervise.AgentInterval, spec.LogPath)
	for _, line := range lastLines(string(out), 6) {
		fmt.Printf("  | %s\n", strings.TrimSpace(line))
	}
	return 0
}

func uninstallAgent() int {
	if runtime.GOOS != "darwin" {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: -uninstall is macOS/launchd only\n")
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+supervise.AgentLabel).Run()
	path := supervise.AgentPlistPath(home)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	fmt.Printf("removed %s\n", path)
	return 0
}
