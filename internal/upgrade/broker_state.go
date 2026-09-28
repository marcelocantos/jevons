// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Every guard that would signal a seat's process, or reap what a broker may
// have parented, asks one question: could a claudia broker own the seats?
// [brokerAvailable] is a bool, and false conflates "nothing is there" with "it
// did not answer in 2s". Under host load (100-200) the second is common, and
// every guard that read false as "no broker" stopped the broker's own client
// (🎯T796, 03:19 pid 4864; 🎯T796.1). So false needs positive evidence that no
// broker listens; a socket that exists but does not answer is a yes.

// brokerSocketState is the seam over the filesystem: does a socket exist that
// could belong to a broker?
type brokerSocketState int

const (
	socketNone  brokerSocketState = iota // no socket, disabled, or nobody listening
	socketMaybe                          // a socket exists and something may own it
)

var brokerSocketProbe = probeBrokerSocket

// brokerMayOwnSeats is true for a broker that answers or that cannot be ruled
// out. It asks the socket first: when one exists the answer is yes whether or
// not the broker replies, so an unanswering broker costs one bounded dial
// instead of re-probing it. Re-probes told "present" from "unknown", which no
// caller distinguishes, and held shutdown ~11 s behind a wedged broker
// (🎯T883). Only when no socket is found is the broker itself asked, in case
// this probe's socket resolution ever diverges from claudia's; that fails fast.
// Tests replace it; it is a var for that reason.
var brokerMayOwnSeats = func() bool {
	if brokerSocketProbe() == socketMaybe {
		return true
	}
	return brokerAvailable()
}

// probeBrokerSocket mirrors claudia's own socket resolution (its broker
// package is internal): CLAUDIA_NO_BROKER off, CLAUDIA_BROKER_SOCKET, else
// $XDG_STATE_HOME or ~/.local/state, then claudia/broker.sock. If the two ever
// diverge the probe sees no socket and the old behaviour returns, so the
// hermetic test pins the resolution.
func probeBrokerSocket() brokerSocketState {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLAUDIA_NO_BROKER"))) {
	case "", "0", "false", "no", "off":
	default:
		return socketNone
	}
	path := brokerSocketPath()
	if path == "" {
		return socketNone
	}
	if _, err := os.Lstat(path); err != nil {
		return socketNone
	}
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		// A stale socket file whose listener is gone refuses at once. A
		// timeout or any other error is the loaded-host case and stays maybe.
		if errors.Is(err, syscall.ECONNREFUSED) {
			return socketNone
		}
		return socketMaybe
	}
	_ = c.Close()
	return socketMaybe
}

func brokerSocketPath() string {
	if p := strings.TrimSpace(os.Getenv("CLAUDIA_BROKER_SOCKET")); p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			return ""
		}
		return abs
	}
	base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "claudia", "broker.sock")
}
