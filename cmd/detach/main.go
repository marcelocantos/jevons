// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// detach runs a command in its own session so the caller's death cannot
// cancel it (🎯T405).
//
//	detach [-log FILE] [-quiet] -- <command> [args…]
//
// Why this exists: restart-daily-jevonsd kills the daily daemon, and the
// daemon's shutdown stops every agent — including the agent that invoked
// the restart. On 2026-08-10 the script died with its invoker five
// seconds after the kill, before it reached the step that starts the
// replacement, and the fleet stayed down until the owner noticed. The
// script has documented that hazard since it was written, as an
// instruction to callers: invoke me detached. A correctness property
// that depends on every caller remembering a convention is not a
// property, and the one caller who forgot took the fleet down.
//
// So the script re-execs itself through this binary unconditionally. The
// work runs in a fresh session with no controlling terminal, so the
// SIGHUP/SIGTERM that tears down the caller's process group never
// reaches it, and a SIGKILL of that group does not either.
//
// The parent still waits and still forwards the exit status, because a
// caller that runs this in the foreground wants to know whether the
// restart worked. Losing that answer when the caller is killed is
// correct — by then there is no caller to tell.
//
// Signals are deliberately NOT forwarded to the child. runlock forwards
// them, which is at best not a mitigation for this failure and at worst
// an amplifier: a SIGTERM sweeping the caller's process group would be
// relayed straight into the bounce it is supposed to survive.
//
// Child output goes to a file, never to an inherited pipe. A pipe whose
// read end dies with the caller kills the child with SIGPIPE on its next
// log line, which would undo the whole exercise. When the caller's own
// stdout is already a regular file — the blessed `>>…restart-daily.log`
// invoke — the child inherits it directly and nothing is duplicated;
// otherwise the child writes to the log and the parent streams it so a
// foreground caller still sees the run.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	logPath := ""
	quiet := false
	var cmdArgs []string

	// Hand-rolled because flag.Parse stops at the first non-flag and the
	// command being wrapped has flags of its own; everything after -- is
	// the child's, verbatim.
	i := 0
	for ; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--":
			i++
			cmdArgs = argv[i:]
			i = len(argv)
		case a == "-quiet" || a == "--quiet":
			quiet = true
		case a == "-h" || a == "--help":
			usage(os.Stdout)
			return 0
		case a == "-log" || a == "--log":
			if i+1 >= len(argv) {
				fmt.Fprintln(os.Stderr, "detach: -log needs a value")
				return 2
			}
			i++
			logPath = argv[i]
		case strings.HasPrefix(a, "-log="), strings.HasPrefix(a, "--log="):
			logPath = a[strings.Index(a, "=")+1:]
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "detach: unknown flag %q\n", a)
			return 2
		default:
			cmdArgs = argv[i:]
			i = len(argv)
		}
	}
	if len(cmdArgs) == 0 {
		usage(os.Stderr)
		return 2
	}

	out, stream, err := childOutput(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "detach: %v\n", err)
		return 2
	}
	defer func() {
		if out != os.Stdout {
			out.Close()
		}
	}()

	// Where the caller's view of the log begins, so streaming shows this
	// run and not the whole history.
	var from int64
	if stream {
		if st, err := out.Stat(); err == nil {
			from = st.Size()
		}
	}
	if !quiet {
		fmt.Fprintf(out, "%s detach: running %s in its own session\n",
			time.Now().Format("2006-01-02T15:04:05-0700"), strings.Join(cmdArgs, " "))
	}

	devnull, err := os.Open(os.DevNull)
	if err != nil {
		fmt.Fprintf(os.Stderr, "detach: %v\n", err)
		return 2
	}
	defer devnull.Close()

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, out, out
	// The whole point: a new session, so the child is in neither the
	// caller's process group nor its session, and no terminal or group
	// signal aimed at the caller can reach it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "detach: %v\n", err)
		return 126
	}

	done := make(chan struct{})
	// Drained before returning: os.Exit does not wait for goroutines, so
	// signalling the tail and returning immediately truncates the child's
	// last lines — including the "OK: … serving" that says the bounce
	// worked. A foreground caller would read a successful restart as an
	// unfinished one.
	drained := make(chan struct{})
	if stream {
		go func() {
			defer close(drained)
			tail(out.Name(), from, done)
		}()
	} else {
		close(drained)
	}
	waitErr := cmd.Wait()
	close(done)
	<-drained

	if waitErr != nil {
		if ee, ok := errors.AsType[*exec.ExitError](waitErr); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "detach: %v\n", waitErr)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: detach [-log FILE] [-quiet] -- <command> [args…]

Runs the command in its own session (setsid) so the caller's death — a
killed agent, a torn-down process group, a closed terminal — cannot
cancel it. The caller still waits and still gets the exit status.

  -log FILE   append the child's output here (default: the caller's own
              stdout when that is a regular file, else a temp file that
              is streamed back)
  -quiet      do not write the "running …" marker

Signals are not forwarded to the child, on purpose (🎯T405).
`)
}

// childOutput picks a file for the child's stdout and stderr, and says
// whether the caller needs it streamed back.
func childOutput(logPath string) (*os.File, bool, error) {
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return nil, false, fmt.Errorf("log dir: %w", err)
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, false, fmt.Errorf("log: %w", err)
		}
		// The blessed invoke already redirects our stdout to this same
		// file. Streaming it back would double every line.
		return f, !sameFile(os.Stdout, f), nil
	}

	// No log named: inherit the caller's stdout when it is a regular
	// file. The descriptor stays valid in the child after the caller
	// dies, which a pipe would not.
	if st, err := os.Stdout.Stat(); err == nil && st.Mode().IsRegular() {
		return os.Stdout, false, nil
	}
	f, err := os.CreateTemp("", "detach-*.log")
	if err != nil {
		return nil, false, fmt.Errorf("temp log: %w", err)
	}
	return f, true, nil
}

func sameFile(a, b *os.File) bool {
	sa, err := a.Stat()
	if err != nil {
		return false
	}
	sb, err := b.Stat()
	if err != nil {
		return false
	}
	return os.SameFile(sa, sb)
}

// tail copies the log from offset to our own stdout until done, then
// drains whatever the child wrote last.
func tail(path string, from int64, done <-chan struct{}) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return
	}
	finishing := false
	for {
		if _, err := io.Copy(os.Stdout, f); err != nil {
			return
		}
		if finishing {
			return
		}
		select {
		case <-done:
			// One more pass so the child's final lines are not lost to
			// the race between its last write and its exit.
			finishing = true
		case <-time.After(100 * time.Millisecond):
		}
	}
}
