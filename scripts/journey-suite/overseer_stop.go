// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 🎯T837: an isolate whose overseer never reaches running used to end the
// suite with "overseer not running yet: [{jevons stopped}]" and nothing
// else — no launch error, no park, no hint that the readiness budget had
// simply run out. Both recorded specimens were the last: a20944c1 spent 42s
// of the 45s budget booting and finished boot 3s before it; 3d88ee5a's Grok overseer was
// still launching 36s after it began. And the one "auto-start failed" line in
// 3d88ee5a ("context canceled") was written AFTER the suite's own interrupt,
// so a reader of the log tail blamed the teardown for the stop it reported.
//
// The cause is read from the isolate daemon's own log, from the bytes this
// start wrote, and before the suite signals the daemon to stop.

// overseerNotRunningError is the readiness probe's verdict when /api/agents
// answers but the overseer is not running.
type overseerNotRunningError struct {
	agents string
}

func (e *overseerNotRunningError) Error() string {
	return "overseer not running yet: " + e.agents
}

// isolateOutageError marks an isolate that never became usable. It is a
// harness outage — the suite never got to assert anything — so it reports
// OUT / exit 2, never a journey verdict.
type isolateOutageError struct {
	err   error
	cause string
}

func (e *isolateOutageError) Error() string {
	return fmt.Sprintf("isolate outage, not a journey verdict: %v — %s", e.err, e.cause)
}

func (e *isolateOutageError) Unwrap() error { return e.err }

// logLine is one slog text-handler line: its attributes by key.
type logLine map[string]string

// parseLogLine reads slog's text format: key=value pairs, values quoted with
// Go escapes when they contain spaces. Lines that are not slog output (the
// suite's own prose, a panic trace) return nil.
func parseLogLine(line string) logLine {
	out := logLine{}
	rest := strings.TrimSpace(line)
	for rest != "" {
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 || strings.ContainsAny(rest[:eq], " \t\"") {
			return nil
		}
		key := rest[:eq]
		rest = rest[eq+1:]
		var val string
		if strings.HasPrefix(rest, `"`) {
			quoted, err := strconv.QuotedPrefix(rest)
			if err != nil {
				return nil
			}
			val, _ = strconv.Unquote(quoted)
			rest = rest[len(quoted):]
		} else if sp := strings.IndexByte(rest, ' '); sp >= 0 {
			val, rest = rest[:sp], rest[sp:]
		} else {
			val, rest = rest, ""
		}
		// slog repeats a key when a record's attrs shadow a built-in one
		// ("budget clamp-down" logs a second level= and msg=); the first
		// occurrence is the record's own.
		if _, seen := out[key]; !seen {
			out[key] = val
		}
		rest = strings.TrimLeft(rest, " ")
	}
	if out["msg"] == "" {
		return nil
	}
	return out
}

// clock renders a line's time= as wall-clock time for the message, falling
// back to the raw value.
func (l logLine) clock() string {
	t, err := time.Parse(time.RFC3339Nano, l["time"])
	if err != nil {
		return l["time"]
	}
	return t.Format("15:04:05.000")
}

// overseerStopReason names why the overseer was not running when the
// readiness wait gave up at gaveUp after waiting budget, from the isolate
// daemon's own log output for this start. It prefers a cause the daemon
// logged — a launch error, a plan-policy park, the daemon's own OVERSEER NOT
// RUNNING diagnosis — and otherwise calls it a start timeout, saying how far
// the start got and when.
func overseerStopReason(log []byte, overseer string, gaveUp time.Time, budget time.Duration) string {
	var launchErr, park, notRunning, launchBegan, launched, booted logLine
	for _, raw := range strings.Split(string(log), "\n") {
		l := parseLogLine(raw)
		if l == nil {
			continue
		}
		msg := l["msg"]
		// Everything after a shutdown is teardown: "context canceled" from an
		// interrupted launch is the suite stopping the daemon, not the cause.
		if msg == "shutting down" {
			break
		}
		switch {
		case msg == "auto-start failed" && l["agent"] == overseer,
			msg == "cockpit: overseer launch failed" && l["name"] == overseer:
			launchErr = l
		case strings.HasPrefix(msg, "plan policy") && l["name"] == overseer &&
			(msg == "plan policy parked" || l["level"] == "WARN" || l["level"] == "ERROR"):
			park = l
		case strings.HasPrefix(msg, "OVERSEER NOT RUNNING") && l["overseer"] == overseer:
			notRunning = l
		case msg == "jevon agent":
			launchBegan = l
		case msg == "agent started" && l["name"] == overseer,
			msg == "cockpit: overseer launched and attached" && l["name"] == overseer:
			launched = l
		case msg == "jevonsd starting":
			booted = l
		}
	}

	var causes []string
	if launchErr != nil {
		causes = append(causes, fmt.Sprintf("launch error at %s: %s", launchErr.clock(), launchErr["err"]))
	}
	if park != nil {
		detail := park["msg"]
		for _, k := range []string{"from", "to", "provider", "reason", "err"} {
			if v := park[k]; v != "" {
				detail += " " + k + "=" + v
			}
		}
		causes = append(causes, fmt.Sprintf("plan-policy park at %s: %s", park.clock(), detail))
	}
	if notRunning != nil {
		causes = append(causes, fmt.Sprintf("daemon reported OVERSEER NOT RUNNING at %s (provider=%s): %s",
			notRunning.clock(), notRunning["provider"], notRunning["likely_cause"]))
	}
	if len(causes) > 0 {
		return "overseer " + overseer + " stopped: " + strings.Join(causes, "; ")
	}

	// No cause logged: the readiness budget ran out on a start still under
	// way. Say how far it got, so a slow boot and a hung launch read
	// differently.
	before := func(l logLine) string {
		t, err := time.Parse(time.RFC3339Nano, l["time"])
		if err != nil || gaveUp.IsZero() {
			return l.clock()
		}
		return fmt.Sprintf("%s, %s before the deadline", l.clock(), gaveUp.Sub(t).Round(100*time.Millisecond))
	}
	var progress []string
	switch {
	case launched != nil:
		progress = append(progress, "overseer launched at "+before(launched)+" but had not reported running")
	case launchBegan != nil:
		progress = append(progress, fmt.Sprintf("overseer launch (provider=%s resume=%s) began at %s and never finished",
			launchBegan["provider"], launchBegan["resume"], before(launchBegan)))
	default:
		progress = append(progress, "overseer launch never began")
	}
	// The daemon listens before it launches the overseer, and logs
	// "jevonsd starting" only once that launch returns: a missing line
	// means boot is still waiting on the overseer, not that nothing served.
	if booted != nil {
		progress = append(progress, "daemon boot finished (\"jevonsd starting\") at "+before(booted))
	} else {
		progress = append(progress, "daemon boot never finished (no \"jevonsd starting\" logged)")
	}
	return fmt.Sprintf("overseer %s stopped: start timeout after %s — %s; no launch error or plan-policy park in the isolate log",
		overseer, budget, strings.Join(progress, "; "))
}
