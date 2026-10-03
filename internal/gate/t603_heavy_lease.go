// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// 🎯T603 — a gate must survive host load, and it must not contribute to it.
// KILLED (🎯T461) already stops a host-OOM'd run from reading as absent or
// as a pass. This half is the other side: several seats each starting a
// heavy `go test` at once is exactly the load that produces that OOM. A
// shared host-pressure lease serialises them so runs queue instead of race.

// HeavyCommand reports whether argv is a heavy package run — a `go test`
// invocation, or a make/gmake target whose name mentions test — the class of
// gate command that claims real memory and can collectively OOM a shared
// host when several run at once. Deliberately broad: matching only a
// specific package (e.g. ./internal/mcpserver) would let any other heavy
// package race unserialised, which defeats the point.
func HeavyCommand(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	switch filepath.Base(argv[0]) {
	case "go":
		for _, a := range argv[1:] {
			if a == "test" {
				return true
			}
		}
		return false
	case "make", "gmake":
		for _, a := range argv[1:] {
			if strings.Contains(a, "test") {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// heavyLockPath is the file whose flock serialises heavy runs. It lives
// inside the gate store so it travels with ~/.jevons/gates rather than being
// a second, easily-forgotten location, and so unrelated stores (as tests use,
// one temp dir per test) never contend with each other.
func heavyLockPath(storeRoot string) string {
	return filepath.Join(storeRoot, "heavy.lock")
}

// AcquireHeavyLease blocks until it holds the exclusive host-pressure lease
// for heavy package runs in storeRoot, then returns a release func. A second
// gate that wants the same lease waits here rather than starting: two `go
// test` processes racing for the same swap is the incident this answers.
// storeRoot == "" means no store is configured (bare Run with no Store, only
// ever right in tests) — the lease is skipped rather than leased into a
// directory nobody chose.
func AcquireHeavyLease(storeRoot string) (func(), error) {
	release, _, err := acquireHeavyLease(storeRoot, "")
	return release, err
}

// The token is an invocation-chain capability, not a global skip switch.
// Only Run's child environment receives it; concurrent Run calls in this
// process still acquire distinct leases. Go exec wrappers preserve environment
// variables even when they close inherited file descriptors (test-web-clean).
// Reuse also requires a response from the owning gate: a dead owner's file
// text and a wrapper-retained descriptor must never authorize nesting.
const heavyLeaseEnv = "JEVONS_GATE_HEAVY_LEASE"

func acquireHeavyLease(storeRoot, inherited string) (func(), string, error) {
	return acquireHeavyLeaseCommand(storeRoot, inherited, nil, "")
}

func acquireHeavyLeaseCommand(storeRoot, inherited string, command []string, cwd string) (func(), string, error) {
	if storeRoot == "" {
		return func() {}, "", nil
	}
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		return nil, "", err
	}
	f, err := os.OpenFile(heavyLockPath(storeRoot), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, "", err
	}
	// First try the real lock: an old token never resurrects a released lease.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	for err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
		if _, seekErr := f.Seek(0, 0); seekErr != nil {
			_ = f.Close()
			return nil, "", seekErr
		}
		owner, readErr := io.ReadAll(io.LimitReader(f, 256))
		if readErr == nil && inherited != "" && string(owner) == inherited && leaseOwnerAlive(inherited) {
			_ = f.Close()
			// The ancestor owns both locking and unlocking. An inner gate must
			// not release its lease while the outer command is still running.
			return func() {}, inherited, nil
		}
		// Retry the live capability as well as the kernel lock: a delayed
		// responder must not turn valid nesting into a permanent deadlock.
		time.Sleep(100 * time.Millisecond)
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		_ = f.Close()
		return nil, "", err
	}
	release := func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
	// Replace the owner record on every acquisition, including the public
	// non-reentrant API, so tokens cannot refer to a previous lock holder.
	// Only the gate retains this listener and lock FD. Both are close-on-exec;
	// no ExtraFiles handoff to arbitrary wrappers or agent processes is allowed.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		release()
		return nil, "", err
	}
	unlock := release
	release = func() { _ = listener.Close(); unlock() }
	token := fmt.Sprintf("%d:%s:%d", os.Getpid(), rand.Text(), listener.Addr().(*net.TCPAddr).Port)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	owner := HeavyLeaseOwner{PID: os.Getpid(), Command: command, CWD: cwd, Started: time.Now(), Token: token}
	data, err := json.Marshal(owner)
	if err == nil {
		err = writeAtomic(heavyLockPath(storeRoot)+".owner", data, 0600)
	}
	if err != nil {
		release()
		return nil, "", err
	}
	if err := f.Truncate(0); err != nil {
		release()
		return nil, "", err
	}
	if _, err := f.WriteAt([]byte(token), 0); err != nil {
		release()
		return nil, "", err
	}
	go serveLeaseOwner(listener, token)
	return release, token, nil
}
