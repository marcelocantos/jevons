// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/marcelocantos/jevons/internal/bouncetiming"
)

func main() {
	os.Exit(run())
}

func run() int {
	daemonLog := flag.String("daemon-log", "", "jevonsd log file (slog text)")
	healthAt := flag.String("health-at", "", "RFC3339 time when /health returned 200 (optional)")
	flag.Parse()
	if *daemonLog == "" {
		fmt.Fprintln(os.Stderr, "bouncetiming: -daemon-log required")
		return 2
	}
	f, err := os.Open(*daemonLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bouncetiming: %v\n", err)
		return 1
	}
	defer f.Close()
	events, err := bouncetiming.Parse(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bouncetiming: %v\n", err)
		return 1
	}
	w, ok := bouncetiming.LastBounceWindows(events)
	if !ok {
		fmt.Fprintln(os.Stderr, "bouncetiming: no loaded-CA → converge window in log")
		return 1
	}
	var down time.Duration
	if *healthAt != "" {
		t, err := time.Parse(time.RFC3339Nano, *healthAt)
		if err != nil {
			t, err = time.Parse(time.RFC3339, *healthAt)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "bouncetiming: health-at: %v\n", err)
			return 1
		}
		down = bouncetiming.ShutdownToHealth(w, t)
	}
	procOK, downOK := bouncetiming.WithinLimits(w, down)
	fmt.Printf("process_to_converge=%s", w.ProcessToConverge.Round(time.Millisecond))
	if !w.Shutdown.IsZero() {
		fmt.Printf(" old_shutdown=%s", w.Shutdown.Format(time.RFC3339Nano))
	}
	if down > 0 {
		fmt.Printf(" shutdown_to_health=%s", down.Round(time.Millisecond))
	}
	fmt.Printf(" process_to_converge_ok=%t", procOK)
	if down > 0 {
		fmt.Printf(" shutdown_to_health_ok=%t", downOK)
	}
	fmt.Println()
	return 0
}
