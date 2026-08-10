// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RunArgs configures one gate run.
type RunArgs struct {
	// Command is the argv to execute. Command[0] is the program, resolved on
	// PATH. There is deliberately no shell: a shell is what introduces the
	// pipeline whose status is not the gate's.
	Command []string
	// Dir is the working directory; empty means the current one.
	Dir string
	// Name labels the run in the attestation; empty derives one from Command.
	Name string
	// Stdout and Stderr receive the command's output as it arrives, so the
	// gate is not a black box while it runs. Nil means os.Stdout/os.Stderr.
	Stdout io.Writer
	Stderr io.Writer
	// Store persists the record and the captured log. Nil runs the gate
	// without leaving evidence, which is only ever right in tests.
	Store *Store
	// Now defaults to time.Now; tests pin it.
	Now func() time.Time
}

// Run executes the gate and returns what it actually did.
//
// The error return is reserved for failures of the gate machinery itself —
// the command could not be started, the record could not be written. A
// command that runs and fails is not an error here: it is a Record with a
// non-zero status and a RED verdict, which is exactly the outcome the caller
// must be able to read.
func Run(args *RunArgs) (*Record, error) {
	if args == nil || len(args.Command) == 0 {
		return nil, errors.New("gate run: no command")
	}
	now := args.Now
	if now == nil {
		now = time.Now
	}
	stdout := args.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := args.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	started := now()
	rec := &Record{
		Name:    gateName(args.Name, args.Command),
		Command: append([]string(nil), args.Command...),
		Dir:     args.Dir,
		Started: started,
	}
	rec.ID = newID(rec.Command, started)

	// One buffer for both streams, guarded, so the captured log reads in the
	// order a human saw it rather than as two interleaved halves.
	var mu sync.Mutex
	var captured bytes.Buffer

	cmd := exec.Command(args.Command[0], args.Command[1:]...)
	cmd.Dir = args.Dir
	cmd.Stdin = nil
	cmd.Stdout = &teeWriter{mu: &mu, buf: &captured, out: stdout}
	cmd.Stderr = &teeWriter{mu: &mu, buf: &captured, out: stderr}

	runErr := cmd.Run()
	rec.Ended = now()

	out := captured.Bytes()
	rec.OutputBytes = len(out)
	rec.OutputSHA256 = digest(out)
	rec.OutputTail = tailOf(out)
	rec.Anomalies = ScanOutput(string(out))

	// The whole point of the package is here: the status comes from the
	// process's own wait status, and anything that is not a clean exit code
	// is recorded as unknown rather than rounded down to zero.
	switch exitErr, isExit := errors.AsType[*exec.ExitError](runErr); {
	case runErr == nil:
		rec.ExitStatus, rec.StatusKnown = 0, true
	case isExit && exitErr.ExitCode() >= 0:
		rec.ExitStatus, rec.StatusKnown = exitErr.ExitCode(), true
	case isExit:
		rec.StatusKnown = false
		rec.StatusNote = "process died without an exit code: " + exitErr.String()
	default:
		rec.StatusKnown = false
		rec.StatusNote = "gate could not run the command: " + runErr.Error()
	}
	rec.Verdict = verdictFor(rec.StatusKnown, rec.ExitStatus, rec.Anomalies)

	if args.Store != nil {
		rec.OutputPath = args.Store.LogPath(rec.ID)
		if err := os.WriteFile(rec.OutputPath, out, 0o644); err != nil {
			return rec, fmt.Errorf("gate run %s: write log: %w", rec.ID, err)
		}
		if err := args.Store.Save(rec); err != nil {
			return rec, err
		}
	}
	return rec, nil
}

// teeWriter forwards to the live stream and to the capture buffer under one
// lock shared by stdout and stderr.
type teeWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
	out io.Writer
}

// Write always reports success: the bytes reached the capture, which is the
// copy the record is built from. A failing live stream must not abort the
// command being gated — losing the pass-through is cosmetic, losing the run
// is not.
func (t *teeWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf.Write(p)
	t.mu.Unlock()
	if t.out != nil {
		_, _ = t.out.Write(p)
	}
	return len(p), nil
}

// gateName derives a short label from the argv when none was given:
// "make test-go" becomes "make-test-go", "go test ./..." becomes "go-test".
func gateName(explicit string, argv []string) string {
	if s := strings.TrimSpace(explicit); s != "" {
		return sanitizeName(s)
	}
	parts := []string{filepath.Base(argv[0])}
	for _, a := range argv[1:] {
		if strings.HasPrefix(a, "-") || strings.ContainsAny(a, "/*.") {
			continue
		}
		parts = append(parts, a)
		if len(parts) >= 3 {
			break
		}
	}
	return sanitizeName(strings.Join(parts, "-"))
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ' || r == '/':
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "gate"
	}
	return name
}

// newID is a short handle for the run, unique enough to tie a pasted
// attestation to a stored record.
func newID(argv []string, at time.Time) string {
	seed := fmt.Sprintf("%s|%d|%d", strings.Join(argv, " "), at.UnixNano(), os.Getpid())
	return digest([]byte(seed))[:8]
}
