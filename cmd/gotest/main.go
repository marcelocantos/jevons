// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// gotest runs `go test` and reports a verdict instead of a transcript.
//
//	gotest [-timeout 20m] [packages...]      # default ./...
//
// Why this exists: `go test ./...` emits thousands of lines of legitimate
// log output from passing tests, and the failure signal is a handful of
// uppercase words scattered through it. A human scans for FAIL and finds
// it. An agent reads every line at the same weight, greps for the shape it
// expects, and — twice on 2026-08-10 — concluded green from a command that
// could not have shown it otherwise:
//
//	go test ./... | grep -v '^ok\|no test files'
//
// That pipeline discards `go test`'s EXIT CODE (the pipeline reports
// grep's), leaving only "I did not see the word FAIL" as evidence. A test
// that had been failing for two commits was recorded as passing twice.
//
// The design rule that follows is the same one this fleet keeps needing
// elsewhere: absence of a signal must never read as success. So:
//
//   - success is a POSITIVE claim with a count, never silence
//   - zero tests executed is a FAILURE, not a clean run
//   - a build failure is a failure, not "no tests to report"
//   - the exit code is the verdict and nothing pipes over it
//   - on failure, the summary names what broke; the transcript goes to a
//     file, so the detail is available without burying the verdict
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// event is one `go test -json` record. Package-level records carry no Test.
type event struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// result accumulates a run. Everything the verdict needs is counted here;
// nothing is inferred from the absence of a line.
type result struct {
	Passed, Failed, Skipped int
	Packages                map[string]bool // package → ok
	FailedTests             []string        // "package\tTest"
	BuildErrors             []string
	transcript              *os.File
}

func run(argv []string) int {
	timeout := "20m"
	var pkgs []string
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "-timeout":
			if i+1 < len(argv) {
				i++
				timeout = argv[i]
			}
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, "usage: gotest [-timeout d] [packages...]")
			return 2
		default:
			pkgs = append(pkgs, argv[i])
		}
	}
	if len(pkgs) == 0 {
		pkgs = []string{"./..."}
	}

	logPath := filepath.Join(os.TempDir(), "gotest-failures.log")
	f, err := os.Create(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gotest: cannot open transcript: %v\n", err)
		return 3
	}
	defer f.Close()

	res := &result{Packages: map[string]bool{}, transcript: f}
	args := append([]string{"test", "-json", "-timeout", timeout}, pkgs...)
	cmd := exec.Command("go", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gotest: %v\n", err)
		return 3
	}
	cmd.Stderr = f
	start := time.Now()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "gotest: %v\n", err)
		return 3
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		res.consume(sc.Bytes())
	}
	// `go test` exits non-zero on any failure. That status is the verdict;
	// it is captured here rather than piped over, which is the whole point.
	waitErr := cmd.Wait()
	elapsed := time.Since(start).Round(time.Millisecond)

	return res.report(os.Stdout, waitErr, elapsed, logPath)
}

func (r *result) consume(line []byte) {
	var ev event
	if json.Unmarshal(line, &ev) != nil {
		// Not a JSON record: `go test` writes build errors as plain text.
		// Keep it — a line we cannot parse is never discarded silently.
		fmt.Fprintf(r.transcript, "%s\n", line)
		return
	}
	switch ev.Action {
	case "output":
		fmt.Fprint(r.transcript, ev.Output)
		if strings.Contains(ev.Output, "[build failed]") ||
			strings.Contains(ev.Output, "cannot find package") {
			r.BuildErrors = append(r.BuildErrors, strings.TrimSpace(ev.Output))
		}
	case "pass":
		if ev.Test == "" {
			r.Packages[ev.Package] = true
		} else {
			r.Passed++
		}
	case "fail":
		if ev.Test == "" {
			r.Packages[ev.Package] = false
		} else {
			r.Failed++
			r.FailedTests = append(r.FailedTests, ev.Package+"\t"+ev.Test)
		}
	case "skip":
		if ev.Test != "" {
			r.Skipped++
		}
	}
}

// report writes the verdict to w and returns the exit code.
//
// It takes a Writer rather than printing directly because its own tests
// exercise it, and a report printed to stdout during `go test` is
// captured as test output — where this tool then read its own fixture
// text as a genuine build failure. The log-noise-as-failure bug, found by
// the harness testing itself.
func (r *result) report(w io.Writer, waitErr error, elapsed time.Duration, logPath string) int {
	okPkgs, badPkgs := 0, 0
	for _, ok := range r.Packages {
		if ok {
			okPkgs++
		} else {
			badPkgs++
		}
	}

	// A build failure is a FAILURE, not an absence of tests. Reported
	// first because it explains why counts may look wrong.
	if len(r.BuildErrors) > 0 {
		fmt.Fprintf(w, "✗ BUILD FAILED — %d package(s) did not compile\n", len(r.BuildErrors))
		for _, e := range dedupe(r.BuildErrors, 10) {
			fmt.Fprintf(w, "    %s\n", e)
		}
		fmt.Fprintf(w, "  full output: %s\n", logPath)
		return 1
	}

	// Nothing ran. Silence here would be indistinguishable from success,
	// which is exactly the confusion this tool exists to prevent — a bad
	// package selector or a build that produced no tests must not look
	// like a clean run.
	if r.Passed+r.Failed+r.Skipped == 0 {
		fmt.Fprintf(w, "✗ NO TESTS EXECUTED — this is a failure, not a clean run\n")
		fmt.Fprintf(w, "  %d package(s) reported; full output: %s\n", len(r.Packages), logPath)
		return 1
	}

	if r.Failed > 0 || badPkgs > 0 || waitErr != nil {
		fmt.Fprintf(w, "✗ %d FAILED, %d passed", r.Failed, r.Passed)
		if r.Skipped > 0 {
			fmt.Fprintf(w, ", %d skipped", r.Skipped)
		}
		fmt.Fprintf(w, " — %d/%d packages ok (%s)\n", okPkgs, okPkgs+badPkgs, elapsed)
		sort.Strings(r.FailedTests)
		for _, ft := range r.FailedTests {
			parts := strings.SplitN(ft, "\t", 2)
			fmt.Fprintf(w, "    %s\n        %s\n", parts[1], parts[0])
		}
		if r.Failed == 0 && waitErr != nil {
			// Non-zero exit with no failing test: a panic, a timeout, or a
			// package-level failure. Never reported as success.
			fmt.Fprintf(w, "    (no individual test failed, but go test exited non-zero: %v)\n", waitErr)
		}
		fmt.Fprintf(w, "  full output: %s\n", logPath)
		return 1
	}

	// Success is a positive claim with numbers in it.
	fmt.Fprintf(w, "✓ %d tests passed", r.Passed)
	if r.Skipped > 0 {
		fmt.Fprintf(w, " (%d skipped)", r.Skipped)
	}
	fmt.Fprintf(w, " across %d packages in %s\n", okPkgs, elapsed)
	return 0
}

func dedupe(in []string, max int) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] || s == "" {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= max {
			break
		}
	}
	return out
}
