// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package noopwedge

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// corpusMaxSessions bounds the scan. Sessions are taken newest first, so the
// bound keeps the check on recent fleet behaviour and off the whole history
// of the machine — an unbounded pass over every project directory read 27,114
// sessions and was killed for memory.
const corpusMaxSessions = 400

// TestCorpusFalsePositiveRate is the measurement that decided 🎯T402's shape,
// kept as a standing check rather than a one-off finding.
//
// It runs stage 1 of the discriminator over this repository's own recent
// session transcripts and asserts that none of them is shape-suspect. That is
// a real assertion, not a tautology: every session it reads belongs to a
// worker that was actually doing something, and a string-alone detector fails
// it outright. When first run over the jevons project directory, 179 sessions
// contained a bare acknowledgement and every single one also contained real
// turns — a 179/179 false-positive rate for the obvious detector.
//
// IT HAS ALREADY EARNED ITS KEEP. The first draft of bareAcks included the
// empty string and bare "ok", and this check failed on 618 sessions across the
// wider corpus: 376 whose turns produced nothing at all (interrupted, not
// answered — 🎯T387's class) and 241 that answered a yes/no question with
// "ok". Both are now excluded by name in the vocabulary, with the counts
// recorded there. Neither would have been caught by the hermetic fixtures.
//
// The corpus is the owner's own session directory, so this skips where it is
// absent (CI, a fresh clone, another machine). It is a ratchet on the local
// path, not a hermetic gate: TestOverBroadMutation carries the same property
// hermetically against a fixture, and that one always runs.
func TestCorpusFalsePositiveRate(t *testing.T) {
	sessions := recentSessions(t)
	if len(sessions) == 0 {
		t.Skip("no local session corpus — hermetic coverage is TestOverBroadMutation")
	}

	var withAck, shapeSuspect, scanned int
	var suspects []string
	for _, path := range sessions {
		turns, err := TurnsFromFile(path)
		if err != nil || len(turns) == 0 {
			continue
		}
		scanned++
		for _, tn := range turns {
			if tn.ToolCalls == 0 && IsBareAck(tn.Text) {
				withAck++
				break
			}
		}
		if ShapeNoOp(turns) {
			shapeSuspect++
			if len(suspects) < 10 {
				suspects = append(suspects, filepath.Base(path))
			}
		}
	}

	t.Logf("corpus: %d sessions scanned, %d contain a bare ack, %d shape-suspect",
		scanned, withAck, shapeSuspect)
	if withAck == 0 {
		t.Skip("corpus contains no bare acknowledgements — nothing to discriminate")
	}
	// A shape-suspect session here is not automatically a bug: a genuinely
	// wedged worker looks exactly like this, and finding one is the detector
	// working. It is still worth failing on, because the measured baseline is
	// zero — so a new one means either a real wedge to investigate or the
	// vocabulary has drifted broad again.
	if shapeSuspect > 0 {
		t.Fatalf("%d/%d sessions shape-suspect (%v) — either a real wedge to "+
			"investigate or bareAcks has grown too broad; measured baseline is 0",
			shapeSuspect, scanned, suspects)
	}
}

// recentSessions returns up to corpusMaxSessions transcripts for this
// repository's own agents, newest first.
func recentSessions(t *testing.T) []string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil
	}
	// Claude names a project directory after its path with separators
	// flattened: /Users/x/work/… → -Users-x-work-….
	repo := filepath.Dir(wd) // .../jevons/internal/noopwedge → .../jevons/internal
	repo = filepath.Dir(repo)
	flat := ""
	for _, r := range repo {
		if r == filepath.Separator || r == '.' || r == '_' {
			flat += "-"
			continue
		}
		flat += string(r)
	}
	paths, err := filepath.Glob(filepath.Join(home, ".claude", "projects", flat, "*.jsonl"))
	if err != nil || len(paths) == 0 {
		return nil
	}
	type entry struct {
		path string
		mod  int64
	}
	entries := make([]entry, 0, len(paths))
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		entries = append(entries, entry{p, fi.ModTime().UnixNano()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod > entries[j].mod })
	if len(entries) > corpusMaxSessions {
		entries = entries[:corpusMaxSessions]
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.path)
	}
	return out
}
