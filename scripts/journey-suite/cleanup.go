// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// 🎯T379: the suite registers a user-scoped MCP server (`jevonsmcp-journey`)
// with the provider CLI, which is global mutable state on the owner's
// machine — every Claude agent started afterwards inherits it. A `defer` is
// not enough to give it back:
//
//   - fatal() ends the process with os.Exit, and os.Exit does not run defers
//   - Ctrl-C and SIGTERM kill the process without unwinding at all
//
// Either way the entry outlives the port, and the next agent to start waits
// on an endpoint nothing serves. That is not hypothetical: it is how
// `jevonsmcp-journey -> 127.0.0.1:13715` came to sit in ~/.claude.json with
// no process behind it, silently costing every subsequent Claude agent those
// tools until 🎯T379 taught agent start to say so.
//
// So teardown is registered here rather than deferred, and every exit path
// in this program goes through exitNow.

// exitCleanup holds teardown functions that must run on ANY exit, including
// abnormal ones. The zero value is ready to use.
type exitCleanup struct {
	mu    sync.Mutex
	fns   []func()
	drain bool
}

// Add registers a teardown function. Functions run in reverse registration
// order, so a later resource is released before the one it depends on.
func (c *exitCleanup) Add(fn func()) {
	if fn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fns = append(c.fns, fn)
}

// Run executes every registered teardown exactly once, in reverse order. It
// is safe to call from a signal handler, from fatal(), and from a defer on
// the happy path — the later calls are no-ops.
//
// A panicking teardown must not strand the ones behind it: releasing the MCP
// registration matters more than any single cleanup succeeding.
func (c *exitCleanup) Run() {
	c.mu.Lock()
	if c.drain {
		c.mu.Unlock()
		return
	}
	c.drain = true
	fns := c.fns
	c.fns = nil
	c.mu.Unlock()

	for i := len(fns) - 1; i >= 0; i-- {
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintln(os.Stderr, "cleanup panicked:", r)
				}
			}()
			fns[i]()
		}()
	}
}

// cleanups is this program's teardown set.
var cleanups exitCleanup

// exitNow runs teardown and terminates. Nothing in this program may call
// os.Exit directly — that is what leaked the registration in the first place.
func exitNow(code int) {
	cleanups.Run()
	os.Exit(code)
}

// catchSignals makes an interrupted run tear down like a completed one.
// Ctrl-C during a long live journey is routine, and it must not cost the
// owner a permanent dead MCP entry.
//
// The conventional exit code for a signal is 128+signum, which keeps a
// deliberate Ctrl-C distinguishable from a suite failure (1) or a provider
// outage (2).
func catchSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-ch
		fmt.Fprintf(os.Stderr, "\nreceived %s — tearing down isolate and MCP registration\n", sig)
		code := 130
		if sig == syscall.SIGTERM {
			code = 143
		}
		exitNow(code)
	}()
}
