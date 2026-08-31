// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T597 — a briefless-seat check resolves the agent's current session, and a
// seat with recent activity is never re-briefed.
//
// 2026-08-31: after a daemon restart, jevons-po read jevons_transcript_read on
// two working seats, got a bare "transcript not found" for session ids that
// differed from the spawn results, applied the 🎯T416 born-stuck instrument
// (registry-named session with no file ⇒ never begun a turn) and re-sent both
// opening briefs in full. Both seats were mid-implementation with uncommitted
// work. The instrument is sound; its input was wrong — a missing file is
// evidence about a path, not about the agent. A session id re-minted at
// restart has no transcript yet precisely BECAUSE it is new, while the agent
// behind it kept working the whole time.
//
// Three defenses live here:
//  1. transcript_read's not-found answer names the paths it searched and
//     whether the current session id post-dates the last daemon restart.
//  2. The same call reports the seat ACTIVE when its report store or workdir
//     shows work since the (re-)mint — born-stuck cannot fire on a live seat.
//  3. jevons_agent_send refuses a full re-brief (spawn-brief envelope, or
//     >1KB opening-brief prose) to a seat with recent activity, naming the
//     evidence and the force_rebrief=true override.

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
)

// RecentActivityWindow bounds how fresh seat evidence must be for the
// re-brief refusal (part 3). Older activity does not block a re-brief: a seat
// silent for longer than this has plausibly stalled, and the PO regains the
// call without the override.
const RecentActivityWindow = 30 * time.Minute

// rebriefProseBound is the acceptance's ">1KB opening-brief prose" line.
const rebriefProseBound = 1024

// workdirScanCap bounds the activity walk so a pathological workdir cannot
// stall a tool call. The walk exits early on the first qualifying touch, so
// the cap only matters for a large tree with nothing touched.
const workdirScanCap = 20000

// workdirSkipDirs are tree components whose churn is not agent work
// (VCS internals, package caches, build output).
var workdirSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "dist.bak": true,
	"bin": true, ".playwright-mcp": true,
}

// SeatActivity is what the daemon observed of a seat since its (re-)mint.
type SeatActivity struct {
	// LastAt is the freshest evidence timestamp (zero when nothing observed).
	LastAt time.Time
	// Evidence names each observation in operator prose.
	Evidence []string
}

// Active reports any evidence at all — the transcript_read ACTIVE verdict.
func (a SeatActivity) Active() bool { return !a.LastAt.IsZero() }

// RecentWithin reports evidence fresh enough to refuse a re-brief.
func (a SeatActivity) RecentWithin(now time.Time, window time.Duration) bool {
	return a.Active() && now.Sub(a.LastAt) <= window
}

// Describe renders the evidence list, or the explicit absence of it.
func (a SeatActivity) Describe() string {
	if !a.Active() {
		return "no stored reports and no workdir files touched since its (re-)mint"
	}
	return strings.Join(a.Evidence, "; ")
}

// noteSeatMinted records when this daemon (re-)minted name's session, the
// baseline "touched since its (re-)mint" is measured from. Unrecorded seats
// (registered before the last restart) fall back to daemon boot.
func (s *Server) noteSeatMinted(name string) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seatMintedAt == nil {
		s.seatMintedAt = map[string]time.Time{}
	}
	s.seatMintedAt[name] = time.Now()
}

// seatMintSince answers the activity baseline for name: the recorded
// (re-)mint when this daemon performed one, else daemon boot. The second is
// deliberately conservative — after a restart the daemon cannot know when a
// still-running seat was originally briefed, and treating boot as the
// baseline means work observed since boot counts as life, which is exactly
// the reading the incident needed.
func (s *Server) seatMintSince(name string) (time.Time, bool) {
	s.mu.Lock()
	minted, ok := s.seatMintedAt[name]
	s.mu.Unlock()
	if ok {
		return minted, true
	}
	return s.bootAt, false
}

// seatActivity gathers the observable evidence that name has worked since its
// (re-)mint: a stored report (🎯T388 store), or a workdir file touched.
func (s *Server) seatActivity(name string) SeatActivity {
	since, _ := s.seatMintSince(name)
	var act SeatActivity

	if dir := s.agentReportStateDir(); dir != "" {
		if rec, err := agentreport.Latest(dir, name); err == nil && rec.At.After(since) {
			act.Evidence = append(act.Evidence, fmt.Sprintf(
				"stored report %s at %s", rec.ID, rec.At.Format(time.RFC3339)))
			if rec.At.After(act.LastAt) {
				act.LastAt = rec.At
			}
		}
	}

	if s.registry != nil {
		if def := s.registry.Def(name); def != nil && strings.TrimSpace(def.WorkDir) != "" {
			if path, mtime, ok := latestWorkdirTouch(def.WorkDir, since); ok {
				act.Evidence = append(act.Evidence, fmt.Sprintf(
					"workdir file %s touched at %s", path, mtime.Format(time.RFC3339)))
				if mtime.After(act.LastAt) {
					act.LastAt = mtime
				}
			}
		}
	}
	return act
}

// latestWorkdirTouch reports one regular file under dir modified after since.
// It exits on the first hit — the question is "has anything been touched?",
// not "what was touched last" — and is bounded by workdirScanCap entries.
// Directory mtimes are ignored (a freshly created empty workdir is not
// activity). Walk errors are skipped, not fatal: an unreadable subtree must
// not turn an activity probe into a tool failure.
func latestWorkdirTouch(dir string, since time.Time) (string, time.Time, bool) {
	var hitPath string
	var hitAt time.Time
	seen := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if workdirSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		if seen > workdirScanCap {
			return filepath.SkipAll
		}
		info, ierr := d.Info()
		if ierr != nil || !info.Mode().IsRegular() {
			return nil
		}
		if info.ModTime().After(since) {
			hitPath, hitAt = path, info.ModTime()
			return filepath.SkipAll
		}
		return nil
	})
	return hitPath, hitAt, hitPath != ""
}

// openingBriefMarkers are phrases a full opening brief carries that ordinary
// fleet chatter does not. Any one of them over rebriefProseBound classifies
// as a re-brief; the envelope arm needs no marker.
var openingBriefMarkers = []string{
	"[Who you are",
	"kind spawn-brief",
	"spawn-brief",
	"standing brief",
	"[Jevons role doctrine]",
	"opening brief",
	"You are jv-",
}

// IsFullRebrief classifies text as a full (re-)brief: a spawn-brief envelope,
// or >1KB of prose carrying opening-brief markers. Pure — the send gate and
// the tapes share it.
func IsFullRebrief(text string) (bool, string) {
	if m, _ := envelope.Parse(text); m != nil && m.Kind == envelope.KindSpawnBrief {
		return true, "a spawn-brief envelope"
	}
	if len(text) > rebriefProseBound {
		for _, marker := range openingBriefMarkers {
			if strings.Contains(text, marker) {
				return true, fmt.Sprintf(
					">1KB opening-brief prose (%d bytes, carries %q)", len(text), marker)
			}
		}
	}
	return false, ""
}

// checkRebriefRefusal is the jevons_agent_send gate (part 3). It answers a
// non-empty refusal message when text is a full re-brief bound for a seat
// with recent activity and force is false. The refusal names the evidence and
// the override, because a refusal whose reason cannot be inspected just gets
// worked around.
func (s *Server) checkRebriefRefusal(name, text string, force bool, now time.Time) string {
	if force {
		return ""
	}
	isRebrief, why := IsFullRebrief(text)
	if !isRebrief {
		return ""
	}
	act := s.seatActivity(name)
	if !act.RecentWithin(now, RecentActivityWindow) {
		return ""
	}
	return fmt.Sprintf(
		"re-brief refused (🎯T597): this message is %s addressed to %q, but that seat shows "+
			"recent activity — %s. A working seat that is re-briefed starts its mission over and "+
			"can discard uncommitted work (that is the 2026-08-31 incident this gate exists for). "+
			"A missing transcript for its session id is evidence about a path, not about the agent "+
			"(🎯T416 input discipline): read jevons_transcript_read %q for the seat-activity verdict "+
			"first. If the seat genuinely needs the brief again, re-send with force_rebrief=true.",
		why, name, act.Describe(), name)
}

// transcriptNotFoundVerdict renders the transcript_read answer for a named
// agent whose session file could not be read (parts 1 and 2). Never a bare
// not-found: it names the session id, the paths searched, whether the id
// post-dates the last daemon restart, and the seat-activity verdict.
func (s *Server) transcriptNotFoundVerdict(agentName, sessionID string, readErr error) (msg string, active bool) {
	var searched []string
	if s.transcript != nil && s.transcript.Locate != nil {
		_, searched = s.transcript.Locate(sessionID)
	}
	searchedLine := "no session roots configured"
	if len(searched) > 0 {
		searchedLine = strings.Join(searched, ", ")
	}

	minted, recorded := s.seatMintSince(agentName)
	mintLine := fmt.Sprintf(
		"this daemon started at %s and did not itself (re-)mint the id, so it may pre-date the restart",
		s.bootAt.Format(time.RFC3339))
	if recorded && !minted.Before(s.bootAt) {
		mintLine = fmt.Sprintf(
			"the id POST-DATES the last daemon restart ((re-)minted %s, daemon started %s) — "+
				"an absent file means no turn since that mint, not that the agent never worked",
			minted.Format(time.RFC3339), s.bootAt.Format(time.RFC3339))
	}

	act := s.seatActivity(agentName)
	if act.Active() {
		return fmt.Sprintf(
			"agent %q has no readable transcript for its CURRENT session %s (%v). "+
				"Searched: %s. Restart context: %s. "+
				"Seat activity: ACTIVE — %s. "+
				"This seat is working: do NOT apply the 🎯T416 born-stuck instrument to it and do "+
				"NOT re-send its opening brief (🎯T597).",
			agentName, sessionDisplay(sessionID), readErr, searchedLine, mintLine, act.Describe()), true
	}
	return fmt.Sprintf(
		"agent %q transcript not found for its CURRENT session %s (%v). "+
			"Searched: %s. Restart context: %s. "+
			"Seat activity: none — %s. "+
			"With no transcript, no stored report and no touched files, the 🎯T416 born-stuck "+
			"reading (never begun a turn) is consistent with what was observed.",
		agentName, sessionDisplay(sessionID), readErr, searchedLine, mintLine, act.Describe()), false
}
