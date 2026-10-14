// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// 🎯T956 — a mint round that loses every seat says so to the parent.
//
// 2026-09-30 14:17-14:29: jevons-po minted nine claude-CLI sonnet workers in
// four rounds; eight were released unbriefed at the fixed 45s window and
// only one survived. The PO then wrote "All 5 confirmed genuinely live" at
// 14:29:03 — fourteen seconds before three of them were stopped — because
// nothing told it otherwise: each release was a silent registry removal, one
// seat at a time, and a PO that issued the starts and moved on had no reason
// to go back and check five rows it believed were done. The daemon knew
// (releaseUnbriefedSeat ran five times for the same parent inside twelve
// seconds) and said nothing.
//
// This tracks, per parent, the seats released unbriefed since that parent's
// last confirmed brief. A short quiet period after the most recent loss
// closes the round — long enough to gather the rest of a round still
// unwinding, short enough that the parent is not kept waiting — and the
// close delivers ONE message naming every seat lost and why. A confirmed
// brief for that parent at any point clears the round entirely: a round
// that produced even one live seat is not the "every seat" shape this target
// is about, and 🎯T762 already covers partial-loss accounting.

// MintRoundQuietEnv overrides the round-close debounce with a Go duration.
// Present for the hermetic suite, which needs the close to happen in test
// time rather than wait out the operator default.
const MintRoundQuietEnv = "JEVONS_MINT_ROUND_QUIET"

// defaultMintRoundQuiet is how long after the most recent loss a parent's
// open round waits for a further loss (or a save) before closing. Seat
// releases in one round land seconds apart — 2026-09-30's five were inside
// twelve seconds of each other — so this sits comfortably above that.
const defaultMintRoundQuiet = 10 * time.Second

func mintRoundQuiet() time.Duration {
	raw := strings.TrimSpace(os.Getenv(MintRoundQuietEnv))
	if raw == "" {
		return defaultMintRoundQuiet
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultMintRoundQuiet
	}
	return d
}

// mintRoundLoss is one seat's unbriefed release, named with the reason that
// decided it.
type mintRoundLoss struct {
	Name   string
	Reason string
}

// mintRoundEntry is one parent's open round: accumulated losses, the timer
// that closes it, and whether the close notice has already gone out (a
// closed round is deleted, but notified guards a race between the timer
// firing and a concurrent noteMintFailed appending one more loss to a round
// this same instant decided to close).
type mintRoundEntry struct {
	losses   []mintRoundLoss
	timer    *time.Timer
	notified bool
}

// mintRoundTracker holds every parent's open round. A Server owns exactly
// one, created lazily.
type mintRoundTracker struct {
	mu      sync.Mutex
	parents map[string]*mintRoundEntry
}

func (s *Server) mintRoundState() *mintRoundTracker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mintRounds == nil {
		s.mintRounds = &mintRoundTracker{parents: map[string]*mintRoundEntry{}}
	}
	return s.mintRounds
}

// noteMintSucceeded clears parent's open round: a confirmed brief means the
// round was not a total loss, so there is nothing to report about it.
func (s *Server) noteMintSucceeded(parent string) {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return
	}
	t := s.mintRoundState()
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.parents[parent]; ok {
		if e.timer != nil {
			e.timer.Stop()
		}
		delete(t.parents, parent)
	}
}

// noteMintFailed records one seat's unbriefed release toward parent's open
// round and (re)schedules the quiet-period close. Each call resets the
// debounce, so a round that keeps losing seats keeps extending until it
// finally goes quiet.
func (s *Server) noteMintFailed(parent, name, reason string) {
	parent = strings.TrimSpace(parent)
	name = strings.TrimSpace(name)
	if parent == "" || name == "" {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "released unbriefed"
	}
	t := s.mintRoundState()
	t.mu.Lock()
	e, ok := t.parents[parent]
	if !ok {
		e = &mintRoundEntry{}
		t.parents[parent] = e
	}
	e.losses = append(e.losses, mintRoundLoss{Name: name, Reason: reason})
	if e.timer != nil {
		e.timer.Stop()
	}
	e.timer = time.AfterFunc(mintRoundQuiet(), func() { s.closeMintRound(parent) })
	t.mu.Unlock()
}

// closeMintRound fires when a parent's round has gone quiet: no further
// loss, and (noteMintSucceeded having had its chance) no save either. It
// delivers one message naming the round and every seat lost in it.
func (s *Server) closeMintRound(parent string) {
	t := s.mintRoundState()
	t.mu.Lock()
	e, ok := t.parents[parent]
	if !ok || e.notified || len(e.losses) == 0 {
		delete(t.parents, parent)
		t.mu.Unlock()
		return
	}
	losses := append([]mintRoundLoss(nil), e.losses...)
	e.notified = true
	delete(t.parents, parent)
	t.mu.Unlock()

	text := FormatMintRoundLossNotice(losses)
	res, err := s.deliverByName(parent, text, OriginAgent, false)
	if !noticeSubmitted(res.Status, err) {
		slog.Info("mint-round-loss notice unsubmitted",
			"component", compAgentLifecycle, "parent", parent, "seats", len(losses),
			"status", res.Status, "err", err)
	}
}

// FormatMintRoundLossNotice is the one parent message a closed total-loss
// round delivers: how many seats, their names, and each one's stop reason —
// so a PO cannot narrate a lost round as live (🎯T692) for want of anything
// ever having told it the round was lost.
func FormatMintRoundLossNotice(losses []mintRoundLoss) string {
	names := make([]string, 0, len(losses))
	for _, l := range losses {
		names = append(names, l.Name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "mint round lost every seat it minted (%d unbriefed): %s",
		len(losses), strings.Join(names, ", "))
	for _, l := range losses {
		fmt.Fprintf(&b, "\n- %s: %s", l.Name, l.Reason)
	}
	return b.String()
}
