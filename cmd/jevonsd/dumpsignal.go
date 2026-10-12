// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"
)

// dumpProfiles is the set a SIGUSR1 writes: every goroutine's stack, and
// the live heap.
var dumpProfiles = []string{"goroutine", "heap"}

// startDumpOnSignal writes goroutine and heap profiles to
// <stateDir>/dumps/<time>-<profile>.txt|.pprof on each sig, and keeps
// running (🎯T1067). SIGQUIT dumps too but kills the daemon, and needs an
// HTTP-independent path: the stall that prompted this had the listener's
// queue full, so a pprof endpoint would not have answered.
func startDumpOnSignal(stateDir string, sig os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig)
	go func() {
		for range ch {
			dir, err := writeDump(filepath.Join(stateDir, "dumps"), time.Now())
			if err != nil {
				slog.Error("diagnostic dump failed", "err", err)
				continue
			}
			slog.Warn("diagnostic dump written (🎯T1067)", "dir", dir, "signal", sig.String())
		}
	}()
}

// writeDump writes one profile file per dumpProfiles entry plus a memstats
// summary into dir, named for at.
func writeDump(dir string, at time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := at.UTC().Format("20060102T150405Z")
	for _, name := range dumpProfiles {
		p := pprof.Lookup(name)
		if p == nil {
			continue
		}
		ext, debug := ".pprof", 0
		if name == "goroutine" {
			ext, debug = ".txt", 2 // full stacks, readable as text
		}
		path := filepath.Join(dir, stamp+"-"+name+ext)
		f, err := os.Create(path)
		if err != nil {
			return dir, err
		}
		werr := p.WriteTo(f, debug)
		cerr := f.Close()
		if werr != nil {
			return dir, werr
		}
		if cerr != nil {
			return dir, cerr
		}
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	summary := fmt.Sprintf("heap_alloc=%d heap_inuse=%d heap_idle=%d heap_released=%d sys=%d num_gc=%d goroutines=%d\n",
		ms.HeapAlloc, ms.HeapInuse, ms.HeapIdle, ms.HeapReleased, ms.Sys, ms.NumGC, runtime.NumGoroutine())
	return dir, os.WriteFile(filepath.Join(dir, stamp+"-memstats.txt"), []byte(summary), 0o600)
}
