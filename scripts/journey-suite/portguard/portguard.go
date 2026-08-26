// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package portguard keeps the journey isolate off the daily-driver port.
// This is harness safety, not a user journey.
package portguard

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/config"
)

// DailyPort is the live owner-driver bind (Universe A).
const DailyPort = config.DailyPort

// DefaultPort is the default Universe B isolate bind.
const DefaultPort = config.JourneyPort

// RefuseDaily returns an error when p is the daily React port or the
// daily vanilla sidecar so the journey suite never binds the owner stream.
func RefuseDaily(p int) error {
	if p == DailyPort {
		return fmt.Errorf("refusing port %d (daily-driver); use %d or -port 0", DailyPort, DefaultPort)
	}
	if p == config.DailyVanillaPort {
		return fmt.Errorf("refusing port %d (daily vanilla sidecar); use %d or -port 0", config.DailyVanillaPort, DefaultPort)
	}
	return nil
}

// ListenPID returns the PID listening on TCP port, or an error when none.
func ListenPID(port int) (int, error) {
	if port <= 0 {
		return 0, fmt.Errorf("no port")
	}
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if line == "" {
		return 0, fmt.Errorf("no listener")
	}
	pid, err := strconv.Atoi(line)
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// ErrIfPortHeld refuses starting an isolate when port is already taken
// (🎯T526). Without this, a bind failure exits the child while waitReady
// adopts the foreign daemon and journey MCP mints into daily ~/.jevons.
func ErrIfPortHeld(port int) error {
	pid, err := ListenPID(port)
	if err != nil || pid <= 0 {
		return nil
	}
	return fmt.Errorf("port %d already in use by pid %d; refusing to talk to a foreign daemon (🎯T526)", port, pid)
}

// ErrIfForeignListener returns an error when port is held by a PID other
// than ourPID (🎯T526). ourPID is the isolate child we just started.
func ErrIfForeignListener(port, ourPID int) error {
	pid, err := ListenPID(port)
	if err != nil || pid <= 0 {
		return nil
	}
	if pid == ourPID {
		return nil
	}
	return fmt.Errorf("port %d held by foreign pid %d, not our %d; aborting before MCP mint (🎯T526)", port, pid, ourPID)
}
