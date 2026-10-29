// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command testpar runs make targets at the same time and fails if any fails
// (🎯T1299). The gate's layers — Go, React unit, Playwright UI, journeys —
// share nothing once their prerequisites are built, so running them one
// after another made the gate the sum of its layers instead of the longest.
//
// Every output line is prefixed with its target and the time since testpar
// started, so the interleaved log still says which layer printed what and
// when, and a summary at the end gives each target's status and duration. GNU Make 3.81 (macOS) has -j but no
// --output-sync, which is why this is not `make -j`.
//
//	go run ./scripts/testpar test-go test-web test-ui-run test-journey-fast-run
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type result struct {
	target string
	err    error
	dur    time.Duration
}

func main() {
	targets := os.Args[1:]
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "usage: testpar <make target>...")
		os.Exit(2)
	}
	makeBin := os.Getenv("MAKE")
	if makeBin == "" {
		makeBin = "make"
	}
	width := 0
	for _, t := range targets {
		width = max(width, len(t))
	}

	began := time.Now()
	var outMu sync.Mutex
	results := make([]result, len(targets))
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Go(func() {
			prefix := fmt.Sprintf("[%-*s] ", width, target)
			cmd := exec.Command(makeBin, target)
			pr, pw := io.Pipe()
			cmd.Stdout, cmd.Stderr = pw, pw
			copied := make(chan struct{})
			go func() {
				defer close(copied)
				sc := bufio.NewScanner(pr)
				sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
				for sc.Scan() {
					outMu.Lock()
					fmt.Printf("%s %s%s\n", elapsed(began), prefix, sc.Text())
					outMu.Unlock()
				}
				_, _ = io.Copy(io.Discard, pr)
			}()
			start := time.Now()
			err := cmd.Run()
			_ = pw.Close()
			<-copied
			results[i] = result{target: target, err: err, dur: time.Since(start)}
			outMu.Lock()
			fmt.Printf("%s %s%s (%s)\n", elapsed(began), prefix, verdict(err), results[i].dur.Round(time.Second))
			outMu.Unlock()
		})
	}
	wg.Wait()

	failed := 0
	fmt.Println("── testpar summary ──")
	for _, r := range results {
		fmt.Printf("  %-*s  %-4s  %s\n", width, r.target, verdict(r.err), r.dur.Round(time.Second))
		if r.err != nil {
			failed++
		}
	}
	if failed > 0 {
		fmt.Printf("testpar: %d of %d target(s) failed: %s\n", failed, len(results), strings.Join(failedTargets(results), ", "))
		os.Exit(1)
	}
}

// elapsed is m:ss since testpar started, the stamp on every line.
func elapsed(began time.Time) string {
	d := time.Since(began).Round(time.Second)
	return fmt.Sprintf("%3d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func verdict(err error) string {
	if err != nil {
		return "FAIL"
	}
	return "ok"
}

func failedTargets(results []result) []string {
	var out []string
	for _, r := range results {
		if r.err != nil {
			out = append(out, r.target)
		}
	}
	return out
}
