// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package hoststray classifies processes a hermetic test or journey isolate
// should not leave behind on the host (🎯T780 / 🎯T846).
package hoststray

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/proclist"
)

// Kind names a stray class for failure messages.
const (
	KindFixtureJevonsd  = "fixture_jevonsd"
	KindJourneyTmux     = "journey_tmux"
	KindOrphanLoadShell = "orphan_load_shell"
)

// Stray is one process the fleet's test harness should not leave running.
type Stray struct {
	PID  int
	PPID int
	Kind string
	Args string
}

// Scan reads the live process table and returns every classified stray.
// An empty slice means none were found.
func Scan() ([]Stray, error) {
	procs, err := proclist.All()
	if err != nil {
		return nil, err
	}
	var strays []Stray
	for _, p := range procs {
		if kind := classify(p.PID, p.PPID, p.Command); kind != "" {
			strays = append(strays, Stray{PID: p.PID, PPID: p.PPID, Kind: kind, Args: p.Command})
		}
	}
	return strays, nil
}

func classify(pid, ppid int, args string) string {
	if pid == os.Getpid() {
		return ""
	}
	if kind := classifyFixtureJevonsd(args); kind != "" {
		return kind
	}
	if kind := classifyJourneyTmux(args); kind != "" {
		return kind
	}
	if kind := classifyOrphanLoadShell(ppid, args); kind != "" {
		return kind
	}
	return ""
}

func classifyFixtureJevonsd(args string) string {
	// The executable itself must be jevonsd. A journey runner names the
	// daemon it drives (`journey-suite -bin ./bin/jevonsd`) from a go-build
	// temp dir, and is not a fixture daemon (2026-10-28 integrate rollback).
	exe, _, _ := strings.Cut(args, " ")
	if !strings.HasSuffix(exe, "bin/jevonsd") {
		return ""
	}
	if strings.Contains(args, ":13705") || strings.Contains(args, " 13705 ") {
		return ""
	}
	if strings.Contains(args, "/.jevons") {
		return ""
	}
	for _, marker := range []string{
		"TestRestartThrashPolicy",
		"TestTheInstalledAgentPATH",
		"TestT580SiblingHEADForcesActivation",
		"TestActivationGate",
		"TestT450",
		"TestT1036",
		"/T/",
	} {
		if strings.Contains(args, marker) {
			return KindFixtureJevonsd
		}
	}
	return ""
}

func classifyJourneyTmux(args string) string {
	if !strings.HasPrefix(args, "tmux") && !strings.Contains(args, " tmux ") {
		return ""
	}
	if !strings.Contains(args, "jevons-journey-") {
		return ""
	}
	if strings.Contains(args, "claudia/tmux.sock") || strings.Contains(args, "claudia-anchor") {
		return KindJourneyTmux
	}
	return ""
}

func classifyOrphanLoadShell(ppid int, args string) string {
	if ppid != 1 {
		return ""
	}
	if strings.Contains(args, "while :; do :; done") {
		return KindOrphanLoadShell
	}
	return ""
}

// HarnessBusy reports whether another docratchet restart or journey run is
// in flight on this host.
func HarnessBusy() bool {
	// A journey run started with `go run` has no "go test" in its argv.
	out, err := exec.Command("pgrep", "-lf", "go test|journey-suite").Output()
	if err != nil && len(out) == 0 {
		return false
	}
	self := strconv.Itoa(os.Getpid())
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == self {
			continue
		}
		rest := strings.Join(fields[1:], " ")
		if strings.Contains(rest, "scripts/docratchet") ||
			strings.Contains(rest, "journey-suite") ||
			strings.Contains(rest, "TestRestartThrashPolicy") {
			return true
		}
	}
	return false
}
