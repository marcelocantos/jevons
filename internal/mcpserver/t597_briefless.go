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
//
// 🎯T721: a spawn-brief with phase implement sent to the seat whose latest
// stored report is a scout-report for that same target is the 🎯T536.3
// handoff, not a mission restart. The discriminator is that terminal
// envelope — not elapsed time or workdir mtime. A genuine re-brief of a
// working seat is still refused.
//
// 🎯T725: a workdir touch of a repo-wide shared file (bullseye.yaml above
// all) is not this seat's activity. The probe either skips those files
// and says so, or it would have to attribute the write to the seat
// before citing it. A seat that edited its own source is still detected.

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

// workdirSharedBasenames are repo-wide files any actor in a shared clone
// may write. A touch of one of these is not evidence that a particular
// seat is working (🎯T725). bullseye.yaml is the load-bearing specimen:
// jevons_target_file and every bullseye commit update it, and workers
// are forbidden from editing it (🎯T546). The list is the named ledger
// only — a broader skip (all yaml, go.mod, AGENTS.md) is how working
// seats start reading idle and the idle-nudge stack fires on them.
var workdirSharedBasenames = map[string]bool{
	"bullseye.yaml": true,
}

// workdirTouchCounts reports whether a workdir path may be cited as
// this seat's activity. Shared ledger files return false. A mutant that
// returns true for every path restores bare mtime-on-any-file and goes
// RED on the 🎯T725 tapes.
func workdirTouchCounts(path string) bool {
	return !workdirSharedBasenames[strings.ToLower(filepath.Base(path))]
}

// SeatActivity is what the daemon observed of a seat since its (re-)mint.
type SeatActivity struct {
	// LastAt is the freshest evidence timestamp (zero when nothing observed).
	LastAt time.Time
	// Evidence names each observation in operator prose.
	Evidence []string
	// SharedSkipped is true when a recent shared file (bullseye.yaml)
	// was seen and not counted as this seat's work (🎯T725).
	SharedSkipped bool
}

// Active reports any evidence at all — the transcript_read ACTIVE verdict.
func (a SeatActivity) Active() bool { return !a.LastAt.IsZero() }

// RecentWithin reports evidence fresh enough to refuse a re-brief.
func (a SeatActivity) RecentWithin(now time.Time, window time.Duration) bool {
	return a.Active() && now.Sub(a.LastAt) <= window
}

// sharedFileExclusion is the 🎯T725 operator note: the probe excluded
// known shared files rather than naming a file the seat may never have
// opened. It is appended whenever a recent shared file was actually
// seen, so a refusal citing a stored report or a real source edit says
// so instead of citing bullseye.yaml.
const sharedFileExclusion = "shared files such as bullseye.yaml are excluded"

// Describe renders the evidence list, or the explicit absence of it.
func (a SeatActivity) Describe() string {
	if !a.Active() {
		if a.SharedSkipped {
			return "no stored reports and no seat-owned workdir files touched since its (re-)mint (" +
				sharedFileExclusion + ")"
		}
		return "no stored reports and no workdir files touched since its (re-)mint"
	}
	out := strings.Join(a.Evidence, "; ")
	if a.SharedSkipped {
		return out + " (" + sharedFileExclusion + ")"
	}
	return out
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
			path, mtime, skipped := latestWorkdirTouch(def.WorkDir, since)
			act.SharedSkipped = skipped
			if path != "" {
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

// latestWorkdirTouch reports one regular file under dir modified after since
// that workdirTouchCounts accepts. Shared ledger files (bullseye.yaml) are
// skipped, not cited: a write by another actor in the same clone is not
// this seat's activity (🎯T725). The walk still exits on the first
// seat-owned hit — the question is "has this seat's tree been touched?",
// not "what was touched last" — and is bounded by workdirScanCap entries.
// Directory mtimes are ignored (a freshly created empty workdir is not
// activity). Walk errors are skipped, not fatal: an unreadable subtree must
// not turn an activity probe into a tool failure.
//
// The third return is true when a recent shared file was seen and not
// counted, so Describe can say so rather than naming that file.
func latestWorkdirTouch(dir string, since time.Time) (string, time.Time, bool) {
	var hitPath string
	var hitAt time.Time
	sharedSkipped := false
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
		if !info.ModTime().After(since) {
			return nil
		}
		if !workdirTouchCounts(path) {
			sharedSkipped = true
			return nil
		}
		hitPath, hitAt = path, info.ModTime()
		return filepath.SkipAll
	})
	return hitPath, hitAt, sharedSkipped
}

// openingBriefMarkers are phrases a full opening brief carries that ordinary
// fleet chatter does not. Any one of them over rebriefProseBound classifies
// as a re-brief; the envelope arm needs no marker.
//
// "spawn-brief" / "kind spawn-brief" are not markers: envelope.Parse already
// catches a real fence, and a >1KB message that merely discusses a brief
// (the 🎯T721 specimen's second refusal) is not itself a re-brief.
var openingBriefMarkers = []string{
	"[Who you are",
	"standing brief",
	"[Jevons role doctrine]",
	"opening brief",
	"You are jv-",
}

// IsFullRebrief classifies text as a full (re-)brief: a spawn-brief envelope,
// or >1KB of prose carrying opening-brief markers. Pure — the send gate and
// the tapes share it.
func IsFullRebrief(text string) (bool, string) {
	if m := parseIncomingEnvelope(text); m != nil && m.Kind == envelope.KindSpawnBrief {
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
//
// 🎯T721: a same-target phase-implement spawn-brief after a scout-report is
// delivered even when the seat shows activity — that activity *is* the scout.
func (s *Server) checkRebriefRefusal(name, text string, force bool, now time.Time) string {
	if force {
		return ""
	}
	isRebrief, why := IsFullRebrief(text)
	if !isRebrief {
		return ""
	}
	if s.phaseAdvanceFromStore(name, text) {
		return ""
	}
	act := s.seatActivity(name)
	if !act.RecentWithin(now, RecentActivityWindow) {
		return ""
	}
	return fmt.Sprintf(
		"re-brief refused (🎯T597): this message is %s addressed to %q, but that seat shows "+
			"recent activity — %s. Re-briefing a working seat can discard uncommitted work "+
			"(that is the 2026-08-31 incident this gate exists for). "+
			"A missing transcript for its session id is evidence about a path, not about the agent "+
			"(🎯T416 input discipline): read jevons_transcript_read %q for the seat-activity verdict "+
			"first. Override: force_rebrief=true. A spawn-brief with phase implement after a "+
			"scout-report for the same target is a 🎯T536.3 handoff, not a re-brief, and does not "+
			"need the override.",
		why, name, act.Describe(), name)
}

// phaseAdvanceFromStore is the 🎯T721 send-path exemption: the seat's own
// latest stored report is the discriminator.
func (s *Server) phaseAdvanceFromStore(name, text string) bool {
	if s == nil {
		return false
	}
	dir := s.agentReportStateDir()
	if dir == "" {
		return false
	}
	rec, err := agentreport.Latest(dir, name)
	if err != nil {
		return false
	}
	return phaseAdvanceHandoff(text, rec.Text)
}

// phaseAdvanceHandoff reports a 🎯T536.3 scout-to-implement send, not a
// mission restart (🎯T721). Incoming must be a spawn-brief whose effective
// phase is implement; latestReport must parse as a scout-report for the
// same target. Workdir mtime and elapsed time are not consulted.
//
// A mutant that returns true for every pair admits every spawn-brief to a
// working seat and goes RED on the T597 workdir / checkpoint tapes.
func phaseAdvanceHandoff(incoming, latestReport string) bool {
	in := parseIncomingEnvelope(incoming)
	if in == nil || in.Kind != envelope.KindSpawnBrief {
		return false
	}
	if envelope.EffectivePhase(in) != envelope.PhaseImplement {
		return false
	}
	want := NormalizeTargetID(in.Target)
	if want == "" {
		return false
	}
	last := parseTerminalEnvelope(latestReport)
	if last == nil || last.Kind != envelope.KindScoutReport {
		return false
	}
	got := NormalizeTargetID(last.Target)
	return got != "" && got == want
}

// parseTerminalEnvelope returns the author's terminal jevons envelope.
// envelope.Parse requires the fence at line 1 (after known prefixes); stored
// reports often have thinking before the fence (the T718 specimen), and they
// also talk ABOUT envelopes after theirs (the T736 specimen). Walk the real
// fence openers from the last backwards and answer the first that parses.
func parseTerminalEnvelope(text string) *envelope.Message {
	if m, err := envelope.Parse(text); m != nil && err == nil {
		return m
	}
	starts := envelope.FenceStarts(text)
	for i := len(starts) - 1; i >= 0; i-- {
		if m := parseFenceAt(text, starts[i]); m != nil {
			return m
		}
	}
	return nil
}

// parseIncomingEnvelope returns the sender's envelope even when prose or
// daemon chrome precedes the fence (🎯T736). The send gate reads the payload
// the caller wrote, and a caller who opens with a line of prose ("Implement
// brief for T731:") still sent a spawn-brief; deciding that on line 1 alone
// makes the gate's verdict depend on the sender's formatting.
func parseIncomingEnvelope(text string) *envelope.Message {
	if m, err := envelope.Parse(text); m != nil && err == nil {
		return m
	}
	for _, i := range envelope.FenceStarts(text) {
		if m := parseFenceAt(text, i); m != nil {
			return m
		}
	}
	return nil
}

// parseFenceAt parses one fence opener. A validation error is not a refusal
// here: kind and target are what the handoff reads, and a real report with a
// slot the schema dislikes is still that report (🎯T736).
func parseFenceAt(text string, off int) *envelope.Message {
	if off < 0 || off >= len(text) {
		return nil
	}
	m, _ := envelope.Parse(text[off:])
	if m == nil || strings.TrimSpace(string(m.Kind)) == "" {
		return nil
	}
	return m
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
