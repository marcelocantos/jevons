// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// runlock serialises a command behind an exclusive file lock (🎯T392.5).
//
//	runlock [-timeout 5m] <lockfile> <command> [args…]
//
// Why this exists: fleet workers each call restart-daily-jevonsd after
// landing a daemon-path change, and nothing stopped two runs overlapping.
// On 2026-08-09 five fired between 21:42 and 21:59; the last two started
// 59 seconds apart, the second killed the first mid-flight, and the daily
// daemon was left down with no supervisor. Every agent turn in flight at
// that moment was cancelled, and cancelled turns bill a full context for
// work that is discarded — 6.6% of the 🎯T392 baseline.
//
// Why Go and not shell: a mutex in bash is a mkdir dance plus a stamp file
// plus stale-lock detection, every line of which is a race (see the
// concurrency rule in the shared bash doctrine). flock(2) is one syscall
// and the kernel releases it when the process dies, including on SIGKILL,
// so a crashed holder cannot wedge every future restart.
//
// Waits rather than skips, deliberately. Coalescing by exiting early would
// be cheaper, but a worker whose binary change landed *after* the running
// restart began would see its change silently not activated — the failure
// 🎯T194 exists to prevent. Waiting costs a few seconds and the wrapped
// script's own already-activated check (🎯T218) makes the second run a
// no-op when the binary did not change.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("runlock", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	timeout := fs.Duration("timeout", 5*time.Minute, "how long to wait for the lock before giving up")
	quiet := fs.Bool("quiet", false, "suppress the waiting notice")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	args := fs.Args()
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: runlock [-timeout d] <lockfile> <command> [args…]")
		return 2
	}
	lockPath, cmdName, cmdArgs := args[0], args[1], args[2:]

	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "runlock: %v\n", err)
		return 2
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runlock: %v\n", err)
		return 2
	}
	defer f.Close()

	waited, err := acquire(f, *timeout, *quiet, lockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runlock: %v\n", err)
		return 75 // EX_TEMPFAIL: the work is still wanted, just not now
	}
	if waited > time.Second && !*quiet {
		fmt.Fprintf(os.Stderr, "runlock: waited %s for %s\n", waited.Round(time.Second), lockPath)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Record the holder so a human reading the lock file can tell who has
	// it. Advisory only — correctness comes from flock, not this content.
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("%d %s\n", os.Getpid(), time.Now().Format(time.RFC3339))), 0)

	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	// Forward signals so a Ctrl-C or a supervisor's SIGTERM stops the child
	// rather than orphaning it while we release the lock underneath it.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "runlock: %v\n", err)
		return 126
	}
	go func() {
		for s := range sigs {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		}
	}()
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "runlock: %v\n", err)
		return 1
	}
	return 0
}

// acquire blocks until the lock is held or timeout elapses. Polling rather
// than a blocking flock so the timeout is honoured on every platform and a
// wedged holder cannot hang the caller forever.
func acquire(f *os.File, timeout time.Duration, quiet bool, path string) (time.Duration, error) {
	start := time.Now()
	notified := false
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return time.Since(start), nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return time.Since(start), err
		}
		if time.Since(start) >= timeout {
			return time.Since(start), fmt.Errorf(
				"timed out after %s waiting for %s — another run still holds it", timeout, path)
		}
		if !notified && !quiet {
			fmt.Fprintf(os.Stderr, "runlock: %s is held; waiting…\n", path)
			notified = true
		}
		time.Sleep(200 * time.Millisecond)
	}
}
