// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// brokerState is what the jevons side can honestly say about a claudia broker.
// [brokerAvailable] is a bool, and false conflates "nothing is there" with "it
// did not answer in 2s". Under host load (100-200) the second is common, and
// every guard that read false as "no broker" stopped the broker's own client
// (🎯T796, 03:19 pid 4864; 🎯T796.1).
type brokerState int

const (
	// brokerAbsent: definitively no broker (switched off, no socket, or a
	// socket file nobody listens on). Only this state may signal a holder.
	brokerAbsent brokerState = iota
	// brokerPresent: the broker answered.
	brokerPresent
	// brokerUnknown: a socket exists but did not answer within the bounded
	// re-probes. Treated as present: refuse and retry, signal nothing.
	brokerUnknown
)

// Re-probes after an unanswered first probe. Each claudia probe is itself
// bounded at 2s, so the worst case is about (retries+1)*2s + retries*pause.
var (
	brokerUnknownReprobes = 3
	brokerUnknownPause    = time.Second
)

// brokerSocketState is the seam over the filesystem: does a socket exist that
// could belong to a broker?
type brokerSocketState int

const (
	socketNone  brokerSocketState = iota // no socket, disabled, or nobody listening
	socketMaybe                          // a socket exists and something may own it
)

var brokerSocketProbe = probeBrokerSocket

// brokerStateNow classifies the broker. Present when it answers; absent only
// on positive evidence there is no listener; unknown otherwise, after bounded
// re-probes.
func brokerStateNow() brokerState {
	if brokerAvailable() {
		return brokerPresent
	}
	if brokerSocketProbe() == socketNone {
		return brokerAbsent
	}
	for range brokerUnknownReprobes {
		time.Sleep(brokerUnknownPause)
		if brokerAvailable() {
			return brokerPresent
		}
	}
	slog.Warn("claudia broker socket exists but did not answer; treating it as present and signalling nothing")
	return brokerUnknown
}

// brokerMayOwnSeats is true for a broker that is present or that could not be
// ruled out. Every guard that would signal a seat's process, or reap what a
// broker may have parented, gates on this rather than on brokerAvailable.
// Tests replace it; it is a var for that reason.
var brokerMayOwnSeats = func() bool { return brokerStateNow() != brokerAbsent }

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
