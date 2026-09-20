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
	sid := strings.TrimSpace(q.SessionID)
	if sid == "" {
		return Location{State: discovery.LookupUnobservable, Reason: "no session id"}
	}
	switch q.Provider {
	case "", claudia.ProviderClaude:
		return locateClaude(sid, q.WorkDir)
	case claudia.ProviderGrok:
		return locateGrok(q.Roots, sid)
	case claudia.ProviderCursor:
		return locateCursor(q)
	default:
		return Location{
			State: discovery.LookupUnobservable,
			Reason: fmt.Sprintf("%s has no durable first-submit transcript this daemon can locate",
				providerLabel(q.Provider)),
		}
	}
}

// Lookup reports when the seat's transcript last moved. One stat, no decode
// (🎯T705).
func Lookup(q Query) Reading {
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

func locateGrok(roots discovery.Roots, sessionID string) Location {
	got := discovery.GrokUpdatesLookup(roots, sessionID)
	out := Location{State: got.State, Reason: got.Reason, Path: got.Path}
	if got.Err != nil && out.Reason != "" {
		out.Reason = fmt.Sprintf("%s: %v", got.Reason, got.Err)
	}
	if out.State == "" {
		out.State = discovery.LookupUnobservable
		out.Reason = "grok lookup returned no state"
	}
	return out
}

func locateCursor(q Query) Location {
	// Pinned claudia v0.40.0 startCursorAgent leaves JSONLPath empty and sets
	// TailJSONL=false, so agent.go keeps the initially computed Claude-shaped
	// path — a phantom T501/T519 already refuse. store.db is opened at
	// session/new|session/load (Launch), not at first Prompt, so its
	// existence is a resume check and cannot prove first-submit creation.
	name := strings.TrimSpace(q.Name)
	if name == "" {
		name = "cursor seat"
	}
	return Location{
		State: discovery.LookupUnobservable,
		Reason: name + ": cursor ACP has no first-submit JSONL contract; " +
			"store.db is a resume check (session/new|load) and the Claude-shaped path is a phantom (T501/T519)",
	}
}

func providerLabel(p claudia.Provider) string {
	if p == "" {
		return "empty provider"
	}
	return string(p)
}
