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

	// 🎯T434: the job's PATH is written into the plist, because launchd's
	// own reaches neither the toolchain the restart script builds its
	// helpers with nor the blurter that would say so if it could not.
	agentPath, missing := supervise.AgentPATH(exec.LookPath, supervise.RestartTools)
	for _, tool := range missing {
		if tool == "go" {
			// Installing anyway would produce a supervisor that cannot
			// restart anything the moment bin/ is cleaned, and cannot
			// report that either. Refuse while a human is watching.
			fmt.Fprintf(os.Stderr, "jevons-watchdog: no `go` on this PATH (%s) — "+
				"the watchdog builds the restart script's helpers with it, so a job installed "+
				"now would refuse every restart. Put the toolchain on PATH and re-run.\n",
				os.Getenv("PATH"))
			return 1
		}
		fmt.Fprintf(os.Stderr, "jevons-watchdog: warning: no %s on this PATH — "+
			"the watchdog will restart the daemon but cannot tell you it happened\n", tool)
	}

	spec := supervise.AgentSpec{
		Binary:   binary,
		Repo:     repo,
		StateDir: state,
		Port:     port,
		LogPath:  supervise.AgentLogPath(state),
		PathEnv:  agentPath,
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
	fmt.Printf("  PATH %s\n", agentPath)

	// One implementation of "make launchd hold this job", shared with the
	// daemon's reinstatement path, so the two cannot drift into different
	// definitions of loaded. It also bootstraps before it boots out, so a
	// failed install leaves the working job alone instead of leaving the
	// machine unsupervised (see supervise.LoadAgent).
	if err := supervise.LoadAgent(path, supervise.AgentLabel); err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	out, err := exec.Command("launchctl", "print", supervise.AgentDomain()+"/"+supervise.AgentLabel).CombinedOutput()
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
	if err := supervise.UnloadAgent(supervise.AgentLabel); err != nil {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	path := supervise.AgentPlistPath(home)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "jevons-watchdog: %v\n", err)
		return 1
	}
	fmt.Printf("removed %s\n", path)
	return 0
}
