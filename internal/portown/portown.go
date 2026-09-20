// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package portown is how jevonsd knows whether it still owns its port (🎯T710).
//
// Binding 127.0.0.1:13705 does not occupy *:13705. On macOS a second process
// can listen on the wildcard beside the loopback bind, and localhost — which
// this repo's own Run section tells everyone to open — prefers IPv6 and
// serves the impostor. The watchdog's 127.0.0.1 /health probe stays green.
//
// Loopback-only is deliberate (🎯T6). This package does not widen the bind.
// It names the foreign pid and the fleet mismatch so the owner hears it
// within one supervision interval, the same class of alarm 🎯T405 gives a
// missing supervisor. A squatter is never an outage restart: bouncing the
// real 127.0.0.1 holder can hand the owner URL to the wildcard process.
package portown

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// NoticeKind is the owner-journal kind for a port conflict.
const NoticeKind = "port-squatter"

// NoticeSubject names the development daemon, matching supervise.OutageSubject.
const NoticeSubject = "development-jevonsd"

// Listener is one TCP LISTEN socket on the supervised port.
type Listener struct {
	PID  int
	Cmd  string
	Addr string // 127.0.0.1:13705, *:13705, [::]:13705, …
}

// Conflict is the foreign listeners that should not be there.
type Conflict struct {
	Squatters []Listener
}

// Quiet is true when nothing foreign is listening.
func (c Conflict) Quiet() bool {
	return len(c.Squatters) == 0
}

// Text names the squatting pids. Empty when quiet.
func (c Conflict) Text() string {
	if c.Quiet() {
		return ""
	}
	parts := make([]string, 0, len(c.Squatters))
	for _, s := range c.Squatters {
		cmd := s.Cmd
		if cmd == "" {
			cmd = "?"
		}
		parts = append(parts, fmt.Sprintf("%s pid %d on %s", cmd, s.PID, s.Addr))
	}
	return "foreign listener on the development port: " + strings.Join(parts, "; ")
}

// Classify reports wildcard (and any non-holder) listeners as squatters.
//
// The 127.0.0.1 holder is "us". A wildcard beside it is the 2026-09-20
// incident. No 127.0.0.1 holder means this is not a T710 alarm — the port
// is unserved, which is 🎯T405's outage path.
func Classify(listeners []Listener) Conflict {
	holders := map[int]bool{}
	for _, l := range listeners {
		if isIPv4Loopback(l.Addr) {
			holders[l.PID] = true
		}
	}
	if len(holders) == 0 {
		return Conflict{}
	}
	var squat []Listener
	seen := map[string]bool{}
	for _, l := range listeners {
		if !isWildcard(l.Addr) && holders[l.PID] {
			continue
		}
		key := strconv.Itoa(l.PID) + "\x00" + l.Addr
		if seen[key] {
			continue
		}
		seen[key] = true
		squat = append(squat, l)
	}
	sort.Slice(squat, func(i, j int) bool {
		if squat[i].PID != squat[j].PID {
			return squat[i].PID < squat[j].PID
		}
		return squat[i].Addr < squat[j].Addr
	})
	return Conflict{Squatters: squat}
}

func addrHost(addr string) string {
	s := strings.TrimSpace(addr)
	s = strings.TrimPrefix(s, "TCP ")
	s = strings.TrimPrefix(s, "TCP6 ")
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i > 0 {
			return s[:i+1]
		}
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[:i]
	}
	return s
}

func isIPv4Loopback(addr string) bool {
	return addrHost(addr) == "127.0.0.1"
}

func isWildcard(addr string) bool {
	h := addrHost(addr)
	switch h {
	case "*", "0.0.0.0", "::", "[::]":
		return true
	default:
		return false
	}
}
