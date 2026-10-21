// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatactivity answers, for one fleet seat, where its transcript is
// and when that transcript last moved (🎯T702).
//
// Locating the transcript is the 🎯T679.1 born-stuck seam: the daemon already
// resolves a Claude projects JSONL, a Grok updates.jsonl (🎯T694), and refuses
// to guess for Cursor. Recency is that same read one field further on, so the
// two live here together and mcpserver's existence verdict delegates to Locate
// rather than keeping a second copy of the provider map.
//
// The read is one os.Stat. 🎯T705 is the standing warning against re-opening a
// live transcript per request: these tapes are multi-megabyte and grow all
// night, and the loops that watch for stalls are the first thing a per-request
// decode starves.
package seatactivity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/spool"
)

// Verdict is what the meter can honestly say. An unreadable meter is unknown,
// never a value (🎯T677) — a seat whose transcript cannot be located must not
// report age zero (indistinguishable from a seat that just moved) nor now.
type Verdict string

const (
	// VerdictUnknown: the transcript could not be located or stat'd. LastMove
	// is the zero time and Age is meaningless; over the wire the age field is
	// null, never 0.
	VerdictUnknown Verdict = "unknown"
	// VerdictKnown: LastMove is the transcript's mtime and Age is its distance
	// from the query clock.
	VerdictKnown Verdict = "known"
)

// Query names the seat whose current session is read. Lookup is by provider +
// session id (+ workdir for Claude). Roots must be the caller's configured
// Grok/Claude stores — zero Roots makes Grok unobservable rather than probing
// the live home.
type Query struct {
	Name      string
	Provider  claudia.Provider
	SessionID string
	WorkDir   string
	Roots     discovery.Roots
	// OMP is the registry sidecar mark. Locate no longer needs it: every
	// named plan is a sidecar seat (🎯T1079).
	OMP bool
	// Now is the clock the age is measured against. Zero means time.Now();
	// tests inject so an age is a computed distance and not whatever the
	// suite happened to run at.
	Now time.Time
}

// Location is where a seat's transcript is, with the honest tri-state of
// 🎯T679.1: Absent only when the store was actually resolved and searched.
type Location struct {
	State  discovery.LookupState
	Reason string
	Path   string
}

// Reading is one recency observation of a seat's transcript.
type Reading struct {
	Verdict Verdict
	// Reason is why the verdict is unknown; empty when known.
	Reason string
	// Path is the transcript that was read, when one was resolved.
	Path string
	// LastMove is the transcript's mtime; zero unless Verdict is known.
	LastMove time.Time
	// Age is LastMove's distance from the query clock; meaningful only when
	// Verdict is known.
	Age time.Duration
}

// DefaultRoots is the on-disk pair the development daemon reads: Grok sessions
// (ordinary, exclusive-MCP, and durable claudia grok-homes) and Claude
// projects. Omitting exclusive-MCP homes made Grok absence unprovable for
// those seats (🎯T679.1); omitting grok-homes marked a live Grok PO born-stuck
// while updates.jsonl grew (🎯T694).
func DefaultRoots() discovery.Roots {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return discovery.Roots{}
	}
	return discovery.Roots{
		GrokSessions:     filepath.Join(home, ".grok", "sessions"),
		GrokHomeSessions: discovery.ExclusiveGrokSessionRoots(),
		ClaudiaGrokHomes: discovery.ClaudiaGrokHomesRoot(),
		ClaudeProjects:   filepath.Join(home, ".claude", "projects"),
	}
}

// Locate resolves the seat's current transcript per provider.
func Locate(q Query) Location {
	if name := strings.TrimSpace(q.Name); name != "" && spool.SeatHasHistory(spool.Dir(), name) {
		return locateSpool(name)
	}
	sid := strings.TrimSpace(q.SessionID)
	if sid == "" {
		return Location{State: discovery.LookupUnobservable, Reason: "no session id"}
	}
	// A plan name is the seat its launch name is (🎯T1079). Every named
	// plan runs on the sidecar since claudia v0.58, so a legacy row storing
	// grok or claude has no vendor transcript that is still its conversation:
	// reading one would report a stale Grok Build store, or "absent" for a
	// Claude JSONL that a sidecar seat never writes, as the seat's truth.
	// Until the sidecar records the seat's first line it is unobservable,
	// never absent: the spool is an optional projection (claudia's
	// CLAUDIA_OMP_SPOOL_DIR), so an empty one proves nothing.
	id := seatprovider.Resolve(q.Provider)
	switch {
	case id.Sidecar:
		return Location{
			State:  discovery.LookupUnobservable,
			Reason: fmt.Sprintf("%s sidecar seat has no spool record yet", id.Launch),
		}
	case id.Plan == seatprovider.Unset:
		return locateClaude(sid, q.WorkDir)
	default:
		return Location{
			State: discovery.LookupUnobservable,
			Reason: fmt.Sprintf("%s has no durable first-submit transcript this daemon can locate",
				providerLabel(q.Provider)),
		}
	}
}

// Lookup reports when the seat's transcript last moved. Ordinarily one
// stat, no decode (🎯T705). Sidecar/OMP seats are the 🎯T893 exception: the
// dated spool file is shared by every sidecar seat writing that day, so the
// file's os.Stat mtime is the day's last write from ANY seat — every seat
// on that file reads back the identical age regardless of which one just
// moved. For those, the per-line "ts" field (indexed once per file-set
// change, not re-decoded per request) is the per-seat recency instead.
func Lookup(q Query) Reading {
	if name := strings.TrimSpace(q.Name); name != "" && spool.SeatHasHistory(spool.Dir(), name) {
		return lookupSpool(name, q.Now)
	}
	loc := Locate(q)
	if loc.State != discovery.LookupPresent || strings.TrimSpace(loc.Path) == "" {
		reason := loc.Reason
		if strings.TrimSpace(reason) == "" {
			reason = string(loc.State)
		}
		return Reading{Verdict: VerdictUnknown, Reason: reason, Path: loc.Path}
	}
	fi, err := os.Stat(loc.Path)
	if err != nil {
		return Reading{
			Verdict: VerdictUnknown,
			Reason:  fmt.Sprintf("cannot stat transcript %s: %v", loc.Path, err),
			Path:    loc.Path,
		}
	}
	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}
	mod := fi.ModTime()
	age := now.Sub(mod)
	if age < 0 {
		// A transcript stamped in the future is clock skew, not a seat that
		// moved a negative number of seconds ago. Report it as freshly moved;
		// the reading stays known because the file was read.
		age = 0
	}
	return Reading{Verdict: VerdictKnown, Path: loc.Path, LastMove: mod, Age: age}
}

// lookupSpool is the 🎯T893 per-seat path for sidecar/OMP seats: the seat's
// own newest "ts" field, never the shared dated file's mtime.
func lookupSpool(seat string, now time.Time) Reading {
	dir := spool.Dir()
	path := spool.LatestPath(dir, seat)
	ts, ok := spool.LatestSeatTime(dir, seat)
	if !ok {
		return Reading{
			Verdict: VerdictUnknown,
			Reason:  "sidecar spool has a file for seat but no readable per-seat ts field",
			Path:    path,
		}
	}
	if now.IsZero() {
		now = time.Now()
	}
	age := now.Sub(ts)
	if age < 0 {
		// Clock skew, not negative age — see the ordinary-path comment above.
		age = 0
	}
	return Reading{Verdict: VerdictKnown, Path: path, LastMove: ts, Age: age}
}

func locateClaude(sessionID, workDir string) Location {
	if strings.TrimSpace(workDir) == "" {
		return Location{State: discovery.LookupUnobservable, Reason: "no workdir"}
	}
	ok, err := claudia.SessionExists(sessionID, workDir)
	if err != nil {
		return Location{
			State:  discovery.LookupUnobservable,
			Reason: fmt.Sprintf("claude session lookup: %v", err),
		}
	}
	if ok {
		return Location{
			State: discovery.LookupPresent,
			Path:  claudia.SessionJSONLPath(sessionID, workDir),
		}
	}
	return Location{State: discovery.LookupAbsent, Reason: "claude JSONL is absent"}
}

func locateSpool(seat string) Location {
	path := spool.LatestPath(spool.Dir(), seat)
	if path == "" {
		return Location{
			State:  discovery.LookupAbsent,
			Reason: "sidecar spool has no records for seat",
		}
	}
	return Location{State: discovery.LookupPresent, Path: path}
}

func providerLabel(p claudia.Provider) string {
	if p == "" {
		return "empty provider"
	}
	return string(p)
}
