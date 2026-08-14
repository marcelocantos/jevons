// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 🎯T441, requirement two. Closing the hole in the writer does nothing about
// the records already through it. Before the allowlist, any first argument
// that was not `last`/`show`/`check` was executed as a command, so a mistyped
// subcommand ran whichever program of that name happened to be on PATH and
// filed the result under the typo: `gate ls` minted `GATE ls exit=0 GREEN`,
// and `gate list` found an unrelated tview demo and minted a RED. Those
// records are lookup-able, which under 🎯T386 means citable, and the GREEN one
// is what `gate last` hands the next reader who asks. A fixed writer over an
// unswept store still has a false green sitting in it.
//
// The sweep therefore runs by predicate rather than over a list of ids: the
// oldest of the four found by hand was five days older than the session that
// noticed, so the store had been quietly accumulating them, and any list is a
// list of the ones somebody happened to look at.

// bareWord reports whether s has the shape of a word typed at gate in the
// hope that gate itself would interpret it: no path, no flag, no shell
// punctuation — just a name.
func bareWord(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, ".") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// SubcommandGuessReason says why rec attests nothing, or "" when it is a gate
// like any other.
//
// Two clauses, and the first is what makes this sound rather than a guess.
//
// Explicit is stamped by the fixed binary on every run it performs, because
// since the allowlist the separated form is the only form that runs anything
// at all. So no record written from here on can match this predicate, however
// bare its command — `gate -- pytest` is a deliberate gate and stays one. The
// records that can match are exactly those written by the binary that had the
// defect, which is the population the sweep exists to clean.
//
// Within that population the second clause identifies the guess: a single
// argument, no arguments of its own, shaped like a word rather than a path.
// A real gate is a runner plus what to run — `make test-go`, `go test ./...`,
// `node scripts/foo_test.js`. Across the 193 records in the daily store when
// this was written, exactly five matched, and all five were typos: three
// `list`, one `ls`, and one `zzz-definitely-not-a-subcommand` typed while
// reproducing the defect.
//
// The residual, stated rather than hidden: a legacy record for a deliberate
// single-word gate would match too. None existed, and the sweep quarantines
// rather than deletes, so such a record would remain readable by id and
// restorable — see Store.Void.
func SubcommandGuessReason(rec *Record) string {
	if rec == nil || rec.Explicit || len(rec.Command) != 1 {
		return ""
	}
	word := rec.Command[0]
	if !bareWord(word) {
		return ""
	}
	return fmt.Sprintf(
		"%q was run as a command by a gate with no subcommand allowlist (🎯T441): "+
			"it is a bare word with no arguments, so what it measured is whatever "+
			"program of that name was on PATH, not a gate",
		word)
}

// SweepResult is one record the sweep judged, and what became of it.
type SweepResult struct {
	Record *Record
	Reason string
	// Voided is false on a dry run, and on a record that could not be moved.
	Voided bool
	Err    error
}

// Sweep finds every citable record that attests nothing and, when void is
// set, moves it out of the citable store.
//
// Dry by default. The sweep is the half of 🎯T441 that removes evidence rather
// than refusing to create it, and a tool that silently discards evidence is
// the wrong shape for this package whatever its predicate — so the default is
// to name the candidates and let a reader look at them.
func (s *Store) Sweep(void bool, now func() time.Time) ([]SweepResult, error) {
	if s == nil {
		return nil, nil
	}
	recs, err := s.Recent(0)
	if err != nil {
		return nil, err
	}
	var out []SweepResult
	for _, rec := range recs {
		reason := SubcommandGuessReason(rec)
		if reason == "" {
			continue
		}
		res := SweepResult{Record: rec, Reason: reason}
		if void {
			voided, err := s.Void(rec.ID, reason, now)
			if err != nil {
				res.Err = err
			} else {
				res.Record, res.Voided = voided, true
			}
		}
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Record.Started.Before(out[j].Record.Started)
	})
	return out, nil
}
