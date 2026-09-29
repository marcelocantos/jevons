// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// One isolate death is one outage, not a column of RED journeys (🎯T839).
//
// Gate a80d7b58: J17's in-suite restart died "overseer not running yet", the
// isolate on 127.0.0.1:64859 never came back, and the fourteen journeys after
// it each reported the dead port as their own verdict — eleven FAIL, three
// OUT, depending on which error text a journey happened to classify. The
// reader went hunting fourteen bugs; there was one.
//
// So the suite checks, after every journey, whether its isolate is still
// there. The journey during which it died is reported once as the suite's
// isolate outage, naming its error and the isolate's own last ERROR lines;
// every later journey that runs while the isolate is still down is OUT, and
// its line points at that first cause. A journey that brings the isolate
// back (J20 and J29 restart it) ends the outage, so a genuine failure after
// a recovery is still FAIL — and a journey that fails with a live isolate is
// FAIL exactly as before.

// isolateProbeTimeout bounds the after-journey dial; a live isolate on
// loopback answers in microseconds.
const isolateProbeTimeout = 2 * time.Second

// isolateOutage is one continuous period with no isolate daemon.
type isolateOutage struct {
	journey string // the journey during which the isolate died
	cause   string // that journey's error (or the probe's, if it passed)
	probe   string // why the isolate was judged dead
	logErr  string // the isolate log's last ERROR lines, if any
	later   int    // journeys that ran while it was still down
}

func (o *isolateOutage) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "isolate died during %s: %s", o.journey, trim(o.cause, 300))
	fmt.Fprintf(&b, " [%s]", trim(o.probe, 200))
	if o.logErr != "" {
		fmt.Fprintf(&b, "; isolate log: %s", o.logErr)
	}
	return b.String()
}

// isolateOutageError is a journey verdict shaped by an isolate outage: the
// journey during which the isolate died (first), or one that ran while it was
// still down. Either way it is OUT, never FAIL.
type isolateOutageError struct {
	outage *isolateOutage
	first  bool
	err    error // the journey's own error; nil when it passed but left the isolate down
}

func (e *isolateOutageError) Error() string {
	if e.first {
		return "ISOLATE OUTAGE — not a journey verdict: " + e.outage.String()
	}
	return fmt.Sprintf("no isolate since %s (first cause: %s) — not a journey verdict: %s",
		e.outage.journey, trim(e.outage.cause, 160), trim(e.err.Error(), 200))
}

func (e *isolateOutageError) Unwrap() error { return e.err }

// isolateAlive reports why the suite's isolate daemon is gone, or nil. A
// missing process means its last start or restart failed (startDaemon stops
// a child that never became ready); an exited process is a crash; a refused
// dial is a daemon no longer answering on its port.
func (s *suite) isolateAlive() error {
	if s.aliveProbe != nil {
		return s.aliveProbe()
	}
	if s.cmd == nil || s.cmd.Process == nil {
		return errors.New("no isolate daemon process: its last start or restart failed")
	}
	if s.cmdWait != nil {
		select {
		case err := <-s.cmdWait:
			s.cmd = nil
			s.cmdWait = nil
			if err == nil {
				err = errors.New("exit 0")
			}
			return fmt.Errorf("isolate daemon exited: %w", err)
		default:
		}
	}
	conn, err := net.DialTimeout("tcp", s.host, isolateProbeTimeout)
	if err != nil {
		return fmt.Errorf("isolate not answering on %s: %w", s.host, err)
	}
	return conn.Close()
}

// classifyIsolate folds the isolate's state after a journey into that
// journey's verdict. It returns the error to report (nil for a pass) and
// records or ends the suite's current isolate outage.
func (s *suite) classifyIsolate(name string, err error) error {
	downAtStart := s.isolateDown
	probeErr := s.isolateAlive()
	if probeErr == nil {
		if downAtStart != nil {
			fmt.Fprintf(s.out(), "isolate back after %s (outage since %s ends)\n", name, downAtStart.journey)
			s.isolateDown = nil
		}
		return err
	}
	if downAtStart != nil {
		if err == nil {
			return nil
		}
		downAtStart.later++
		return &isolateOutageError{outage: downAtStart, err: err}
	}
	cause := "journey passed but left the isolate down"
	if err != nil {
		cause = err.Error()
	}
	o := &isolateOutage{
		journey: name,
		cause:   cause,
		probe:   probeErr.Error(),
		logErr:  lastLogErrors(s.logPath, 2),
	}
	s.isolateDown = o
	s.isolateOutages = append(s.isolateOutages, o)
	return &isolateOutageError{outage: o, first: true, err: err}
}

// lastLogErrors returns the last n level=ERROR records of the isolate log,
// without their timestamps, joined with " | ".
func lastLogErrors(path string, n int) string {
	if path == "" {
		return ""
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var errs []string
	for _, line := range strings.Split(string(body), "\n") {
		_, rest, ok := strings.Cut(line, "level=ERROR")
		if !ok {
			continue
		}
		errs = append(errs, trim(strings.TrimSpace(rest), 300))
	}
	if len(errs) > n {
		errs = errs[len(errs)-n:]
	}
	return strings.Join(errs, " | ")
}
