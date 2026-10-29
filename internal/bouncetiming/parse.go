// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package bouncetiming parses jevonsd slog lines for 🎯T794 bounce windows.
package bouncetiming

import (
	"bufio"
	"io"
	"strings"
	"time"
)

// Markers are slog msg= values the restart script and acceptance criteria name.
const (
	MsgLoadedCA        = "loaded CA from disk"
	MsgShuttingDown    = "shutting down"
	MsgConvergeStarted = "cockpit: converge loop started"
)

// Event is one parsed log line with a recognized marker.
type Event struct {
	When time.Time
	Msg  string
	Line string
}

// Windows holds durations for the last complete bounce in a log tail.
type Windows struct {
	ProcessToConverge time.Duration
	ProcessStart      time.Time
	ConvergeStart     time.Time
	Shutdown          time.Time
}

// Limits from 🎯T794 acceptance (proposal bar).
const (
	MaxProcessToConverge = 5 * time.Second
	MaxShutdownToHealth  = 8 * time.Second
)

// Parse scans r for slog text lines and returns recognized events in order.
func Parse(r io.Reader) ([]Event, error) {
	var out []Event
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		when, msg, ok := parseSlogLine(line)
		if !ok {
			continue
		}
		switch msg {
		case MsgLoadedCA, MsgShuttingDown, MsgConvergeStarted:
			out = append(out, Event{When: when, Msg: msg, Line: line})
		}
	}
	return out, sc.Err()
}

// LastBounceWindows finds the last new-process boot in events.
func LastBounceWindows(events []Event) (Windows, bool) {
	var w Windows
	lastCA := -1
	for i, ev := range events {
		if ev.Msg == MsgLoadedCA {
			lastCA = i
		}
	}
	if lastCA < 0 {
		return w, false
	}
	w.ProcessStart = events[lastCA].When
	for i := lastCA + 1; i < len(events); i++ {
		if events[i].Msg == MsgConvergeStarted {
			w.ConvergeStart = events[i].When
			w.ProcessToConverge = w.ConvergeStart.Sub(w.ProcessStart)
			break
		}
	}
	if w.ConvergeStart.IsZero() {
		return w, false
	}
	for i := lastCA - 1; i >= 0; i-- {
		if events[i].Msg == MsgShuttingDown {
			w.Shutdown = events[i].When
			break
		}
	}
	return w, true
}

// ShutdownToHealth completes downtime when health became ready at healthAt.
func ShutdownToHealth(w Windows, healthAt time.Time) time.Duration {
	if w.Shutdown.IsZero() || healthAt.IsZero() || !healthAt.After(w.Shutdown) {
		return 0
	}
	return healthAt.Sub(w.Shutdown)
}

// WithinLimits reports whether w meets 🎯T794 proposal thresholds.
func WithinLimits(w Windows, shutdownToHealth time.Duration) (processOK, downtimeOK bool) {
	processOK = w.ProcessToConverge > 0 && w.ProcessToConverge <= MaxProcessToConverge
	if shutdownToHealth > 0 {
		downtimeOK = shutdownToHealth <= MaxShutdownToHealth
	}
	return processOK, downtimeOK
}

func parseSlogLine(line string) (time.Time, string, bool) {
	if !strings.HasPrefix(line, "time=") {
		return time.Time{}, "", false
	}
	rest := line[len("time="):]
	ts, rest, ok := cutField(rest)
	if !ok {
		return time.Time{}, "", false
	}
	when, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		when, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return time.Time{}, "", false
		}
	}
	for rest != "" {
		key, val, next, ok := cutKV(rest)
		if !ok {
			break
		}
		if key == "msg" {
			return when, val, true
		}
		rest = next
	}
	return time.Time{}, "", false
}

func cutField(s string) (string, string, bool) {
	if s == "" {
		return "", "", false
	}
	if s[0] == '"' {
		end := strings.Index(s[1:], `"`)
		if end < 0 {
			return "", "", false
		}
		return s[1 : 1+end], s[1+end+1:], true
	}
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		return s, "", true
	}
	return s[:i], s[i+1:], true
}

func cutKV(s string) (key, val, rest string, ok bool) {
	s = strings.TrimLeft(s, " ")
	if s == "" {
		return "", "", "", false
	}
	eq := strings.IndexByte(s, '=')
	if eq < 0 {
		return "", "", "", false
	}
	key = s[:eq]
	valPart := s[eq+1:]
	if valPart == "" {
		return "", "", "", false
	}
	if valPart[0] == '"' {
		end := strings.Index(valPart[1:], `"`)
		if end < 0 {
			return "", "", "", false
		}
		val = valPart[1 : 1+end]
		rest = valPart[1+end+1:]
		return key, val, rest, true
	}
	val, rest, ok = cutField(valPart)
	if !ok {
		return "", "", "", false
	}
	return key, val, rest, true
}
